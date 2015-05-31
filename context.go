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

	if c.DatabaseType == "sqlite3" {
		err := os.MkdirAll(path.Dir(c.DatabaseConnection), 0777)
		checkError(err, "could not create database parent directory 'data'")
	}

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
	if c.sessionStore == nil {
		// TODO define admin URL in one place
		sessionStore, err := south.New(c.SessionKey, c.URLPrefix+"/admin/")
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
	checkError(err, "failed to parse template")

	return tpl
}

func (c *Context) GetTemplateModified(name string) time.Time {
	var t time.Time

	_, paths := c.getTemplatePaths(name)
	for _, p := range paths {
		st, err := os.Stat(p)
		checkError(err, "failed to stat template file")

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
		f, err := os.Open(path.Join(c.BlogPath, "skins", skin, "skin.json"))
		checkError(err, "failed to open skin configuration file")

		buf, err := ioutil.ReadAll(f)
		checkError(err, "failed to read skin configuration file")

		skinJson := SkinJson{Pages: make(map[string]SkinPage)}
		err = json.Unmarshal(buf, &skinJson)
		checkError(err, "failed to parse skin configuration file")

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
		files[i] = path.Join(c.BlogPath, "skins", page.skin, fn)
	}

	return page.Files[0], files
}

func (c *Context) Close() {
	err := c.db.Close()
	checkError(err, "could not close database")
}
