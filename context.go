package main

import (
	"encoding/json"
	"html/template"
	"io/ioutil"
	"os"
	"time"

	"database/sql"
	_ "github.com/mattn/go-sqlite3"

	"github.com/aykevl/south"
)

type Context struct {
	*Config
	root          string
	templateInfos map[string]TemplateInfo
	db            *sql.DB
	router        *Router
	sessionStore  *south.Store
}

type TemplateInfo struct {
	Files []string `json:"templates"`
}

func NewContext(root string) *Context {
	var c Context
	c.Config = loadConfig(root)

	// Set up database connection.
	db, err := sql.Open(c.DatabaseType, c.DatabaseConnection)
	if err != nil {
		panic(err)
	}
	c.db = db

	c.router = NewRouter(&c)

	return &c
}

// SessionStore returns the session store (lazy load)
func (c *Context) SessionStore() *south.Store {
	if (c.sessionStore == nil) {
		// TODO define admin URL in one place
		sessionStore, err := south.New(c.SessionKey, c.SiteRoot + "/admin/")
		checkError(err, "could not create session store")
		c.sessionStore = sessionStore
	}
	return c.sessionStore
}

func (c *Context) GetTemplate(name string) *template.Template {
	name, paths := c.getTemplatePaths(name)

	tpl := template.New(name)
	tpl.Funcs(funcMap)

	_, err := tpl.ParseFiles(paths...)
	if err != nil {
		internalError("failed to parse template", err)
	}

	return tpl
}

func (c *Context) GetTemplateModified(name string) time.Time {
	var t time.Time

	_, paths := c.getTemplatePaths(name)
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			internalError("failed to stat template file", err)
		}

		t = lastTime(t, st.ModTime())
	}

	return t
}

// loadTemplateInfos reads info about templates if it hasn't been loaded
func (c *Context) loadTemplateInfos() {
	if (c.templateInfos != nil) {
		return
	}

	f, err := os.Open(c.TemplateDirectory + "/templates.json")
	if err != nil {
		internalError("failed to open theme configuration file:", err)
	}

	buf, err := ioutil.ReadAll(f)
	if err != nil {
		internalError("failed to read theme configuration file", err)
	}

	c.templateInfos = make(map[string]TemplateInfo)
	err = json.Unmarshal(buf, &c.templateInfos)
	if err != nil {
		internalError("failed to parse theme configuration file", err)
	}
}

func (c *Context) getTemplatePaths(name string) (string, []string) {
	c.loadTemplateInfos()

	info, ok := c.templateInfos[name]
	if !ok {
		raiseError("could not find template " + name)
	}

	files := make([]string, len(info.Files))
	for i, fn := range info.Files {
		files[i] = c.TemplateDirectory + "/" + fn
	}

	return info.Files[0], files
}

func (c *Context) Close() {
	c.db.Close()
}
