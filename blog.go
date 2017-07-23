package main

import (
	"encoding/json"
	"html/template"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/cgi"
	"net/http/fcgi"
	"os"
	"path"
	"time"

	"database/sql"
	_ "github.com/mattn/go-sqlite3"

	"github.com/aykevl/south"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	"github.com/gorilla/securecookie"
)

type Blog struct {
	*Config
	skinPages    map[string]SkinPage
	extraCSS     []string
	extraJS      []string
	icons        []SkinIcon
	skins        []string // list of [skin, parent skins...]
	db           *sql.DB
	router       http.Handler // request router including middleware (CSRF protection etc.)
	mux          *mux.Router  // underlying request router
	sessionStore *south.Store
}

type SkinPage struct {
	skin  string
	Files []string `json:"templates"`
}

type SkinJson struct {
	Parent   string              `json:"parent"`
	Pages    map[string]SkinPage `json:"pages"`
	ExtraCSS []string            `json:"extraCSS"`
	ExtraJS  []string            `json:"extraJS"`
	Icons    []SkinIcon          `json:"icons"`
}

type SkinIcon struct {
	Asset string `json:"asset"`
	Sizes string `json:"sizes"`
}

type TemplateData struct {
	name  string
	files []string
}

func NewBlog(root string) *Blog {
	var b Blog
	b.Config = loadConfig(root)

	if b.DatabaseType == "sqlite3" {
		err := os.MkdirAll(path.Dir(b.DatabaseConnection), 0777)
		checkError(err, "could not create database parent directory 'data'")
	}

	// Set up database connection.
	db, err := sql.Open(b.DatabaseType, b.DatabaseConnection)
	if err != nil {
		panic(err)
	}
	b.db = db

	b.mux = mux.NewRouter()
	sub := b.mux
	if b.URLPrefix != "" {
		sub = b.mux.PathPrefix(b.URLPrefix).Subrouter()
	}

	sub.HandleFunc("/", BlogIndexHandler).Name("index")
	sub.HandleFunc("/{year:[0-9]{4}}/{month:0[0-9]|1[0-2]}/{name:[a-z0-9]+(-[a-z0-9]+)*}", PageViewHandler)
	sub.HandleFunc("/admin/", AdminHandler).Name("admin")
	admin, _ := sub.Get("admin").URLPath()
	sub.Handle("/admin", http.RedirectHandler(admin.Path, http.StatusMovedPermanently))
	sub.HandleFunc("/admin/edit/new{id:post|page}", PageEditHandler)
	sub.HandleFunc("/admin/edit/{id:[1-9][0-9]*}", PageEditHandler)
	sub.HandleFunc("/admin/edit/{id:[1-9][0-9]*}/preview", PagePreviewHandler)
	sub.HandleFunc("/archive/", ArchiveHandler).Name("archive")
	sub.HandleFunc("/feed.xml", FeedHandler).Name("feed")
	archive, _ := sub.Get("archive").URLPath()
	sub.Handle("/archive", http.RedirectHandler(archive.Path, http.StatusMovedPermanently))
	sub.HandleFunc("/assets/{name}", AssetHandler)
	sub.HandleFunc("/{name:[a-z0-9]+(-[a-z0-9]+)*}", PageViewHandler)
	sub.HandleFunc("/{page:.*}", NotFound) // when no route matches: 404 error

	b.router = csrf.Protect(b.CSRFKey, csrf.Secure(b.Secure))(b.mux)

	return &b
}

func (b *Blog) serveCGI() {
	err := cgi.Serve(b.router)
	checkError(err, "failed to serve CGI")
}

func (b *Blog) serveFastCGI() {
	err := os.Remove(b.FastCGISocketPath)
	if !os.IsNotExist(err) {
		checkWarning(err, "could not remove existing socket file")
	}
	socket, err := net.Listen("unix", b.FastCGISocketPath)
	checkError(os.Chmod(b.FastCGISocketPath, 0660), "could not chmod fcgi socket file")
	checkError(err, "could not open fcgi socket file")
	fcgi.Serve(socket, b.router)
}

func (b *Blog) serveHTTP(addr string) {
	err := http.ListenAndServe(addr, b.router)
	checkError(err, "could not bind to HTTP server address")
}

func (b *Blog) generateSessionKey() {
	sessionKey, err := south.GenerateKey()
	checkError(err, "could not generate session key")
	b.SessionKey = sessionKey
	b.Config.Update()
}

func (b *Blog) generateCSRFKey() {
	key := securecookie.GenerateRandomKey(32)
	if key == nil {
		raiseError("could not geterate CSRF token")
	}
	b.CSRFKey = key
	b.Config.Update()
}

// SessionStore returns the session store (lazy load)
func (b *Blog) SessionStore() *south.Store {
	if b.sessionStore == nil {
		adminURL, _ := b.mux.Get("admin").URLPath()
		sessionStore, err := south.New(b.SessionKey, adminURL.Path)
		checkError(err, "could not create session store")
		b.sessionStore = sessionStore
	}
	return b.sessionStore
}

func (b *Blog) GetTemplate(name string) *template.Template {
	tplData := b.getTemplateData(name)

	tpl := template.New(tplData.name)
	tpl.Funcs(funcMap)

	_, err := tpl.ParseFiles(tplData.files...)
	checkError(err, "failed to parse template")

	return tpl
}

func (b *Blog) GetTemplateModified(name string) time.Time {
	var t time.Time

	tplData := b.getTemplateData(name)
	for _, p := range tplData.files {
		st, err := os.Stat(p)
		checkError(err, "failed to stat template file")

		t = lastTime(t, st.ModTime())
	}

	return t
}

// loadSkin reads info about templates if it hasn't been loaded
func (b *Blog) loadSkin() {
	if b.skinPages != nil {
		return
	}
	b.skinPages = make(map[string]SkinPage)

	skin := b.Skin
	for skin != "" {
		b.skins = append(b.skins, skin)
		f, err := os.Open(path.Join(b.BlogPath, "skins", skin, "skin.json"))
		checkError(err, "failed to open skin configuration file")

		buf, err := ioutil.ReadAll(f)
		checkError(err, "failed to read skin configuration file")

		skinJson := SkinJson{Pages: make(map[string]SkinPage)}
		err = json.Unmarshal(buf, &skinJson)
		checkError(err, "failed to parse skin configuration file")

		for name, page := range skinJson.Pages {
			if _, ok := b.skinPages[name]; !ok {
				page.skin = skin
				b.skinPages[name] = page
			}
		}

		for _, css := range skinJson.ExtraCSS {
			b.extraCSS = append(b.extraCSS, css)
		}
		for _, js := range skinJson.ExtraJS {
			b.extraJS = append(b.extraJS, js)
		}

		if skinJson.Icons != nil {
			b.icons = skinJson.Icons
		}

		skin = skinJson.Parent
	}
}

func (b *Blog) getTemplateData(name string) TemplateData {
	b.loadSkin()

	page, ok := b.skinPages[name]
	if !ok {
		raiseError("could not find template " + name)
	}

	files := make([]string, len(page.Files))
	for i, fn := range page.Files {
		files[i] = path.Join(b.BlogPath, "skins", page.skin, fn)
	}

	return TemplateData{
		name:  page.Files[0],
		files: files,
	}
}

func (b *Blog) Close() {
	err := b.db.Close()
	checkError(err, "could not close database")
}
