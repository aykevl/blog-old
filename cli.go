package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"io/ioutil"
	"net/mail"
	"os"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/howeyc/gopass"
)

type Command struct {
	call        func(*Context, []string)
	parameters  int
	description string
}

var commands map[string]Command

func init() {
	// Do this inside init() because otherwise there would be an initialization
	// loop: http://code.google.com/p/go/issues/detail?id=1817
	// (This works as intended and is not a bug in the compiler.)
	cmds := map[string]Command{
		"help":    Command{commandList, 0, "List all available commands"},
		"install": Command{commandInstall, 0, "Install blog"},
		"import":  Command{commandImportDB, 1, "Import stored data from folder - overwrites existing data!"},
		"export":  Command{commandExportDB, 1, "Export stored data to folder - overwrites existing data!"},
		"adduser": Command{commandAddUser, 2, "Add user to the database.\nUsage: adduser <email> <fullname>"},
		"keygen":  Command{commandKeygen, 0, "Create (or overwrite) session key."},
		"secure":  Command{commandSecure, 1, "Toggle security setting (on, off)"},
	}
	commands = cmds
}

func commandList(ctx *Context, _ []string) {
	fmt.Println("Available commands:")
	for name, cmd := range commands {
		fmt.Printf("   %-10s %s\n", name, strings.Replace(cmd.description, "\n", "\n              ", -1))
	}
}

func commandInstall(ctx *Context, _ []string) {
	// The structure of the SQL table.
	tables := map[string][]struct {
		name     string
		datatype string
	}{
		"pages": {
			{"id", "INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL"},
			{"text", "TEXT DEFAULT ''"},
			{"name", "TEXT UNIQUE DEFAULT ''"},
			{"title", "TEXT DEFAULT ''"},
			{"type", "INTEGER DEFAULT 0"},
			{"summary", "TEXT DEFAULT ''"},
			{"created", "INTEGER DEFAULT 0"},
			{"published", "INTEGER DEFAULT 0"},
			{"modified", "INTEGER DEFAULT 0"},
		},
		"users": {
			{"id", "INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL"},
			{"email", "TEXT UNIQUE"},
			{"passwordHash", "TEXT"},
			{"fullname", "VARCHAR DEFAULT ''"},
		},
	}

	// Queries that must be executed after the tables have been installed.
	// None of these queries should have any effect when they're run multiple
	// times.
	fixups := []struct {
		action string
		sql    string
	}{
		{
			"update page type",
			"UPDATE pages SET type=1 WHERE type=0",
		},
	}

	// Fetch all table names in the database.

	tablesInDB := make(map[string]bool)

	rows, err := ctx.db.Query("SELECT name FROM sqlite_master WHERE type='table'")
	if err != nil {
		internalError("could not query table names", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		err := rows.Scan(&name)
		if err != nil {
			internalError("could not query table name", err)
		}
		tablesInDB[name] = true
	}

	// Insert all missing tables, and update outdated tables.
	for name, columns := range tables {
		if !tablesInDB[name] {
			// Table does not exist, add it now.
			var columnsSql []string
			for _, column := range columns {
				columnsSql = append(columnsSql, column.name+" "+column.datatype)
			}

			createTableSql := "CREATE TABLE " + name + " (" + strings.Join(columnsSql, ", ") + ")"
			fmt.Println("Creating table:", name)
			_, err := ctx.db.Exec(createTableSql)
			if err != nil {
				internalError("could not create table '"+name+"'", err)
			}

			continue
		}

		// Query all columns currently in this table.
		rows, err := ctx.db.Query("SELECT * FROM " + name + " LIMIT 0")
		if err != nil {
			internalError("could not query table column names", err)
		}
		columnsInDB, err := rows.Columns()
		if err != nil {
			internalError("could not fetch table column names", err)
		}
		columnsMapInDB := make(map[string]bool)
		for _, column := range columnsInDB {
			columnsMapInDB[column] = true
		}

		// Add columns to this table that do not yet exist.
		for _, column := range columns {
			if columnsMapInDB[column.name] {
				continue
			}

			fmt.Printf("Adding column to table '%s': %s\n", name, column.name)
			addColumnSql := "ALTER TABLE " + name + " ADD COLUMN " + column.name + " " + column.datatype
			_, err := ctx.db.Exec(addColumnSql)
			checkError(err, "could not add column to database")
		}
	}

	// Apply all fixups. These may be needed after updates.
	for _, fixup := range fixups {
		result, err := ctx.db.Exec(fixup.sql)
		checkError(err, "could not ececute update: "+fixup.action+" (SQL: "+fixup.sql+")")

		rowsAffected, err := result.RowsAffected()
		checkError(err, "could not fetch RowsAffected()")
		if rowsAffected > 0 {
			fmt.Printf("Applied update: %s (%d rows affected).\n", fixup.action, rowsAffected)
		}
	}

	// Generate public/private key pair if it does not yet exist.
	if ctx.SessionKey == nil {
		fmt.Println("Generating session sign key")
		generateSessionKey(ctx)
	}
}

func commandImportDB(ctx *Context, args []string) {
	postsDirectory := args[0]

	checkNameStmt, err := ctx.db.Prepare(
		"SELECT id FROM pages WHERE name=?")
	if err != nil {
		internalError("failed to prepare statement", err)
	}

	insertPageStmt, err := ctx.db.Prepare(
		"INSERT INTO pages (text, name, title, created, published, modified) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		internalError("failed to prepare statement", err)
	}

	updatePageStmt, err := ctx.db.Prepare(
		"UPDATE pages SET text=?, title=?, created=?, published=?, modified=? WHERE id=?")
	if err != nil {
		internalError("failed to prepare statement", err)
	}

	files, err := ioutil.ReadDir(postsDirectory)
	if err != nil {
		internalError("failed to read directory containing posts", err)
	}

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".markdown") {
			continue
		}

		fp, err := os.Open(path.Join(postsDirectory, file.Name()))
		if err != nil {
			internalError("failed to open page "+file.Name(), err)
		}
		defer fp.Close()

		msg, err := mail.ReadMessage(fp)
		if err != nil {
			internalError("failed to read page "+file.Name(), err)
		}

		body, err := ioutil.ReadAll(msg.Body)
		if err != nil {
			internalError("failed to read page markdown "+file.Name(), err)
		}

		var title string
		var text string
		if body[0] == '#' && body[1] == ' ' {
			end := 1
			for ; end < len(body); end++ {
				if body[end] == '\n' {
					break
				}
			}

			title = strings.TrimSpace(string(body[2:end]))
			text = strings.TrimLeftFunc(string(body[end:]), unicode.IsSpace)
		}

		dateHeaders := [3]string{"Created", "Published", "Modified"}
		dates := [3]time.Time{}

		for i, header := range dateHeaders {
			if msg.Header.Get(header) == "" {
				continue
			}

			date, err := time.Parse(time.RFC3339, msg.Header.Get(header))
			if err != nil {
				internalError("failed to read "+header+" timestamp for page "+file.Name(), err)
			}

			dates[i] = date
		}

		name := msg.Header.Get("Name")

		row := checkNameStmt.QueryRow(name)
		if err != nil {
			internalError("failed to execute SQL query", err)
		}

		var pageId int64
		err = row.Scan(&pageId)

		if err == sql.ErrNoRows {
			// This page does not yet exist in the database, insert it now.
			fmt.Println("importing:", name)
			_, err := insertPageStmt.Exec(text, name, title, exportTime(dates[0]), exportTime(dates[1]), exportTime(dates[2]))
			if err != nil {
				internalError("failed to insert page into DB", err)
			}

		} else if err != nil {
			internalError("failed to fetch data from DB", err)

		} else {
			// row does exist, update the data
			fmt.Println("updating: ", name)
			_, err := updatePageStmt.Exec(text, title, exportTime(dates[0]), exportTime(dates[1]), exportTime(dates[2]), pageId)
			if err != nil {
				internalError("failed to update page", err)
			}
		}
	}
}

func commandExportDB(ctx *Context, args []string) {
	postsDirectory := args[0]

	// TODO add support for other page types
	for _, post := range PagesFromQuery(ctx, PAGE_TYPE_POST, FETCH_ALL, "", "") {
		fmt.Println("exporting:", post.Name)

		filename := post.Name + ".markdown"
		if !post.Published.IsZero() {
			// published post
			filename = post.Published.Format("2006-01-02-") + filename
		}
		filepath := path.Join(postsDirectory, filename)

		fp, err := os.Create(filepath + ".tmp")
		if err != nil {
			internalError("failed to create file", err)
		}

		output := bufio.NewWriter(fp)
		fmt.Fprintln(output, "Name:", post.Name)
		fmt.Fprintln(output, "Content-Type: text/markdown; charset=utf-8")

		keys := [...]string{"Created", "Published", "Modified"}
		times := [...]time.Time{post.Created, post.Published, post.Modified}
		for i, t := range times {
			if t.IsZero() {
				continue
			}
			fmt.Fprintln(output, keys[i]+":", t.Format(time.RFC3339))
		}

		fmt.Fprintln(output)
		fmt.Fprintln(output, "#", post.Title)
		fmt.Fprintln(output)
		output.WriteString(post.Text)
		output.Flush()

		err = fp.Close()
		if err != nil {
			internalError("failed to close file: "+filename+".tmp", err)
		}

		err = os.Rename(filepath+".tmp", filepath)
		if err != nil {
			internalError("failed to rename file: "+filename+".tmp", err)
		}
	}
}

func commandAddUser(ctx *Context, args []string) {
	email := args[0]
	name := args[1]

	var userId int64
	row := ctx.db.QueryRow("SELECT id FROM users WHERE email=?", email)
	err := row.Scan(&userId)
	if err != sql.ErrNoRows {
		if err != nil {
			internalError("could not check for user email", err)
		}
		fmt.Println("Email address already exists in database.")
		return
	}

	var password string
	for i := 0; ; i++ {
		if i >= 3 {
			return
		}

		fmt.Printf("Password for new user:")
		password = string(gopass.GetPasswd())
		fmt.Printf("Repeat password:")
		password2 := string(gopass.GetPasswd())

		if len(password) < 8 {
			// TODO password strength checking using zxcvbn or similar
			fmt.Println("Use a password of at least 8 characters.")
		} else if password != password2 {
			fmt.Println("Passwords don't match.")
		} else {
			break
		}
	}

	hash := storePassword(password)

	_, err = ctx.db.Exec("INSERT INTO users (email, fullname, passwordHash) VALUES (?, ?, ?)", email, name, hash)
	if err != nil {
		internalError("failed to add user", err)
	}
}

func commandKeygen(ctx *Context, _ []string) {
	generateSessionKey(ctx)
}

func commandSecure(ctx *Context, args []string) {
	v := strings.ToLower(args[0])
	switch v {
	case "on", "yes", "1":
		ctx.Insecure = false
		fmt.Println("Enabled enforcing security.")
	case "off", "no", "0":
		ctx.Insecure = true
		fmt.Println("Disabled enforcing security.")
	default:
		fmt.Fprintln(os.Stderr, `Invalid boolean. Use "on" or "off".`)
		os.Exit(1)
	}

	ctx.Config.Update()
}

func handleCLI(ctx *Context) {
	if len(os.Args) == 0 {
		panic("os.Args should have at least one element")
	}
	if len(os.Args) == 1 {
		fmt.Fprintln(os.Stderr, "Provide at least one command.")
		commandList(ctx, nil)
		os.Exit(1)
	}

	cmd, ok := commands[os.Args[1]]
	if !ok {
		fmt.Printf("I don't know command '%s'.\n", os.Args[1])
		commandList(ctx, nil)
		os.Exit(1)
	}

	if len(os.Args) < cmd.parameters+2 {
		fmt.Fprintf(os.Stderr, "Not enough parameters for command '%s'.\n", os.Args[1])
		os.Exit(1)
	} else if len(os.Args) > cmd.parameters+2 {
		fmt.Fprintf(os.Stderr, "Too much parameters for command '%s'.\n", os.Args[1])
		os.Exit(1)
	}

	cmd.call(ctx, os.Args[2:])
}