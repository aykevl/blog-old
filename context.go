package main

import (
	"encoding/json"
	"html/template"
	"io/ioutil"
	"os"
	"path"
	"time"

	"database/sql"
	_ "github.com/mattn/go-sqlite3"

	"github.com/aykevl/south"
)

type Context struct {
	*Config
	root         string
	skinPages    map[string]SkinPage
	skins        []string // list of [skin, parent skins...]
	db           *sql.DB
	router       *Router
	sessionStore *south.Store
}

type SkinPage struct {
	skin  string
	Files []string `json:"templates"`
}

type SkinJson struct {
	Parent string              `json:"parent"`
	Pages  map[string]SkinPage `json:"pages"`
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

// loadSkin reads info about templates if it hasn't been loaded
func (c *Context) loadSkin() {
	if c.skinPages != nil {
		return
	}
	c.skinPages = make(map[string]SkinPage)

	skin := c.Skin
	for skin != "" {
		c.skins = append(c.skins, skin)
		f, err := os.Open(path.Join(c.BlogRoot, "skins", skin, "skin.json"))
		if err != nil {
			internalError("failed to open skin configuration file:", err)
		}

		buf, err := ioutil.ReadAll(f)
		if err != nil {
			internalError("failed to read skin configuration file", err)
		}

		skinJson := SkinJson{Pages: make(map[string]SkinPage)}
		err = json.Unmarshal(buf, &skinJson)
		if err != nil {
			internalError("failed to parse skin configuration file", err)
		}

		for name, page := range skinJson.Pages {
			if _, ok := c.skinPages[name]; !ok {
				page.skin = skin
				c.skinPages[name] = page
			}
		}
		skin = skinJson.Parent
	}
}

func (c *Context) getTemplatePaths(name string) (string, []string) {
	c.loadSkin()

	page, ok := c.skinPages[name]
	if !ok {
		raiseError("could not find template " + name)
	}

	files := make([]string, len(page.Files))
	for i, fn := range page.Files {
		files[i] = path.Join(c.BlogRoot, "skins", page.skin, fn)
	}

	return page.Files[0], files
}

func (c *Context) Close() {
	err := c.db.Close()
	if err != nil {
		internalError("could not close database", err)
	}
}
