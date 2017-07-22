package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"time"

	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
)

type Values map[string]string

type Viewer interface {
	Output()
}

type Response struct {
	data                map[string]interface{}
	tpl                 string
	errorCode           int
	CookieAuthenticated bool
}

func NewResponse() *Response {
	var res Response
	res.data = make(map[string]interface{}, 3)
	res.data["base"] = blog.URLPrefix
	res.data["siteTitle"] = blog.SiteTitle
	res.data["logo"] = blog.Logo
	res.data["assets"] = blog.URLPrefix + blog.AssetsPrefix
	res.data["admin"] = blog.Origin + blog.URLPrefix + "/admin"
	// these should be moved to template blocks in Go 1.6
	res.data["extraCSS"] = blog.extraCSS
	res.data["extraJS"] = blog.extraJS
	res.data["icons"] = blog.icons
	return &res
}

// Output serves the page, implementing some web server logic. It gives proper
// responses to If-Modified_since requests.
// lastModified is the Last-Modified date of the data added inside the view.
// Output may also add some more and output a different Last-Modified header,
// but if lastModified is nil, no Last-Modified HTTP header will be outputted.
func (res *Response) Output(w http.ResponseWriter, r *http.Request, lastModified time.Time) {

	menu := PagesFromQuery(blog, PAGE_TYPE_STATIC, FETCH_TITLE, "published != 0", "ORDER BY title DESC")
	res.data["menu"] = menu

	h := w.Header()

	if !lastModified.IsZero() {
		// This only adds a Last-Modified header to views that explicitly add a
		// Last-Modified timestamp. Otherwise we might end up with serving old
		// invalid Last-Modified dates.

		lastModified = lastTime(lastModified, blog.GetTemplateModified(res.tpl).Truncate(time.Second), menu.LastModified())

		// These headers must be served at all times, even when sending a 304
		// Not Modified reply.
		// See: http://tools.ietf.org/html/rfc7232#section-4.1
		//     The server generating a 304 response MUST generate any of the
		//     following header fields that would have been sent in a 200 (OK)
		//     response to the same request: Cache-Control, Content-Location, Date,
		//     ETag, Expires, and Vary.

		if res.CookieAuthenticated {
			h.Set("Cache-Control", "private")
			h.Set("Vary", "Cookie")
		} else {
			h.Set("Cache-Control", "max-age=60,s-maxage=5")
		}
	}

	// Test whether we can serve a 304 Not Modified reply.
	if !lastModified.IsZero() && res.errorCode == 0 {
		if equalLastModified(lastModified, r) {
			w.WriteHeader(304) // Not Modified
			return
		}
	}

	tpl := blog.GetTemplate(res.tpl)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)

	err := tpl.Execute(gz, res.data)
	checkError(err, "failed to get view output")

	gz.Close()

	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Encoding", "gzip")
	h.Set("Content-Length", strconv.Itoa(buf.Len()))

	if !lastModified.IsZero() {
		h.Set("Last-Modified", httpLastModified(lastModified))
	}

	if res.errorCode != 0 {
		w.WriteHeader(res.errorCode)
	}

	if r.Method != "HEAD" {
		w.Write(buf.Bytes())
	}
}

func OutputStatic(w http.ResponseWriter, r *http.Request, contentType string, p string) {
	f, err := os.Open(p)
	checkError(err, "OutputStatic: could not open")
	st, err := f.Stat()
	checkError(err, "OutputStatic: could not stat file")

	h := w.Header()
	h.Set("Cache-Control", "max-age=3600,s-maxage=5")

	if equalLastModified(st.ModTime(), r) {
		w.WriteHeader(304) // Not Modified
		return
	}

	if len(contentType) < 1 {
		panic("contentType must have a minimum length of 1")
	}

	if contentType[0] == '.' {
		switch contentType {
		case ".css":
			contentType = "text/css"
		case ".js":
			contentType = "text/javascript"
		default:
			checkError(err, "OutputStatic: unknown extension "+contentType)
		}
	}

	h.Set("Content-Type", contentType+"; charset=utf-8")
	h.Set("Content-Encoding", "gzip")
	h.Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	h.Set("Last-Modified", httpLastModified(st.ModTime()))

	if r.Method != "HEAD" {
		n, err := io.Copy(w, f)
		checkError(err, "could not copy file to output")
		if n != st.Size() {
			//raiseError("OutputStatic: size doesn't match with stat output " + strconv.Itoa(int(n)))
		}
	}
}

func AssetHandler(w http.ResponseWriter, r *http.Request) {
	values := mux.Vars(r)
	name := values["name"]
	if name == "" {
		NotFound(w, r) // or permission denied?
		return
	}

	ext := path.Ext(name)

	if ext != ".js" && ext != ".css" {
		NotFound(w, r)
		return
	}

	outpath := path.Join(blog.WebRoot, blog.AssetsPrefix, name)
	if _, err := os.Stat(outpath + ".gz"); err == nil {
		// The file already exists. That means the server isn't well-configured
		// or there is a race between creating a file and serving another via
		// the blog.
		OutputStatic(w, r, ext, outpath+".gz")
		return
	} else if !os.IsNotExist(err) {
		checkError(err, "could not stat asset file")
	}

	blog.loadSkin()
	for i, skin := range blog.skins {
		var cmd *exec.Cmd // input via command
		var reader io.Reader
		var err error

		// TODO clean up duplicate code

		switch ext {
		case ".js":
			// Only open input JavaScript file
			infile, err := os.Open(path.Join(blog.BlogPath, "skins", skin, name))
			if os.IsNotExist(err) {
				continue
			}
			checkError(err, "failed to open JavaScript file")
			defer infile.Close()
			reader = infile

		case ".css":
			// Start converting input SCSS file to CSS
			scssPath := path.Join(blog.BlogPath, "skins", skin, name[:len(name)-len(ext)]+".scss")
			_, err = os.Stat(scssPath)
			if os.IsNotExist(err) {
				continue
			}
			checkError(err, "failed to stat SCSS file")

			args := []string{"--default-encoding", "utf-8", "--no-cache"}
			for j := i; j < len(blog.skins); j++ {
				args = append(args, "-I", path.Join(blog.BlogPath, "skins", blog.skins[j]))
			}
			args = append(args, scssPath)

			// Debian package for command: ruby-sass
			cmd = exec.Command("scss", args...)
			reader, err = cmd.StdoutPipe()
			checkError(err, "could not get stdout")
			checkError(cmd.Start(), "could not run scss")

		default:
			panic("unreachable")
		}

		err = os.Remove(outpath + ".tmp")
		if !os.IsNotExist(err) {
			checkError(err, "could not remove temporary file")
		}
		err = os.Remove(outpath + ".gz.tmp")
		if !os.IsNotExist(err) {
			checkError(err, "could not remove temporary file")
		}
		// There is a race here: two processes could be creating these files at
		// the same time.
		outfile, err := os.OpenFile(outpath+".tmp", os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
		checkError(err, "failed to open output asset file")
		outgzfile, err := os.OpenFile(outpath+".gz.tmp", os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
		checkError(err, "failed to open output gz asset file")

		gzwriter := gzip.NewWriter(outgzfile)
		// Write the normal and the GZ files at once!
		tee := io.TeeReader(reader, gzwriter)

		_, err = io.Copy(outfile, tee)
		checkError(err, "could not compress and write asset file")
		gzwriter.Close()

		if cmd != nil {
			// It is important to check whether the command finished
			// successfully, otherwise we might end up with invalid SCSS files.
			checkError(cmd.Wait(), "could not finish command")
		}

		// This can be implemented faster by seeking and outputting the file,
		// and then doing the sync+close+rename. Unfortunately, that doesn't
		// seem to work so easily...
		checkError(outfile.Sync(), "could not sync data to tmpfile")
		checkError(outgzfile.Sync(), "could not sync data to gzip tmpfile")
		checkError(outfile.Close(), "could not close tmpfile")
		checkError(outgzfile.Close(), "could not close gzip tmpfile")
		checkError(os.Rename(outpath+".tmp", outpath), "could not rename tmpfile")
		checkError(os.Rename(outpath+".gz.tmp", outpath+".gz"), "could not rename gzip tmpfile")

		OutputStatic(w, r, ext, outpath+".gz")
	}
}

func BlogIndexHandler(w http.ResponseWriter, r *http.Request) {
	res := NewResponse()

	res.tpl = "blogindex"

	posts := PagesFromQuery(blog, PAGE_TYPE_POST, FETCH_TITLE, "published!=0", "ORDER BY published DESC LIMIT 10")
	res.data["posts"] = posts

	res.Output(w, r, posts.LastModified())
}

func PageViewHandler(w http.ResponseWriter, r *http.Request) {
	res := NewResponse()

	page := PageFromQuery(blog, PAGE_TYPE_NONE, FETCH_ALL, "name=? AND published!=0", "", mux.Vars(r)["name"])
	if page == nil {
		NotFound(w, r)
		return
	}

	switch page.Type {
	case PAGE_TYPE_POST:
		res.tpl = "blogpost"
	case PAGE_TYPE_STATIC:
		res.tpl = "page"
	default:
		raiseError("unknown page type while determining template")
	}

	res.data["page"] = page
	res.data["title"] = page.Title

	res.Output(w, r, page.LastModified())
}

func ArchiveHandler(w http.ResponseWriter, r *http.Request) {
	res := NewResponse()
	res.tpl = "archive"

	posts := PagesFromQuery(blog, PAGE_TYPE_POST, FETCH_TITLE, "published!=0", "ORDER BY published DESC")
	res.data["posts"] = posts

	res.Output(w, r, posts.LastModified())
}

func NewAuthenticatedResponse(w http.ResponseWriter, r *http.Request) *Response {
	// Require authenticated views to be of the canonical origin.
	if blog.OriginURL.Host != r.Host || // host can also mean host:port
		(r.URL.Scheme != "" && blog.OriginURL.Scheme != r.URL.Scheme) { // only set using FastCGI?
		u := *r.URL
		u.Scheme = blog.OriginURL.Scheme
		u.Host = blog.OriginURL.Host
		w.Header().Set("Location", u.String())
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(303)
	}
	user, err := NewUser(blog, w, r)
	if user == nil {
		view := NewResponse()
		view.tpl = "login"
		if err == ErrInvalidUser {
			view.data["loginerror"] = "user"
		} else if err == ErrInvalidToken {
			view.data["loginerror"] = "token"
		} else if err == ErrExpiredToken {
			view.data["loginerror"] = "expired"
		} else if err == ErrRedirect {
			// The response has already been sent, exit this HTTP request.
			return nil
		} else {
			checkError(err, "unknown user login error")
		}
		view.data[csrf.TemplateTag] = csrf.TemplateField(r)
		view.Output(w, r, time.Time{})
		return nil
	}

	view := NewResponse()
	view.data["user"] = user
	view.CookieAuthenticated = true

	view.data[csrf.TemplateTag] = csrf.TemplateField(r)

	return view
}

func AdminHandler(w http.ResponseWriter, r *http.Request) {
	res := NewAuthenticatedResponse(w, r)
	if res == nil {
		return
	}

	res.tpl = "admin"

	drafts := PagesFromQuery(blog, PAGE_TYPE_POST, FETCH_TITLE, "published==0", "ORDER BY modified DESC")
	res.data["drafts"] = drafts

	published := PagesFromQuery(blog, PAGE_TYPE_POST, FETCH_TITLE, "published!=0", "ORDER BY published DESC")
	res.data["published"] = published

	menuUnpublished := PagesFromQuery(blog, PAGE_TYPE_STATIC, FETCH_TITLE, "published == 0", "ORDER BY title DESC")
	res.data["menuUnpublished"] = menuUnpublished

	res.Output(w, r, lastTime(drafts.LastModified(), published.LastModified(), menuUnpublished.LastModified()))
}

func PageEditHandler(w http.ResponseWriter, r *http.Request) {
	res := NewAuthenticatedResponse(w, r)
	if res == nil {
		return
	}

	var page *Page

	values := mux.Vars(r)
	if values["id"] == "post" {
		page = &Page{Type: PAGE_TYPE_POST}
	} else if values["id"] == "page" {
		page = &Page{Type: PAGE_TYPE_STATIC}
	} else {
		page = PageFromQuery(blog, PAGE_TYPE_NONE, FETCH_ALL, "id=?", "", values["id"])
		if page == nil {
			NotFound(w, r)
			return
		}
	}

	if r.Method == "POST" {
		// Page.Id will get set during the update.
		newPage := page.Id == 0

		page.Update(blog, r.PostFormValue("name"), r.PostFormValue("title"), r.PostFormValue("summary"), r.PostFormValue("text"))

		if r.PostFormValue("publish") != "" {
			page.Publish(blog)
			w.Header().Set("Location", blog.URLPrefix+page.Url())
		} else if r.PostFormValue("unpublish") != "" {
			page.Unpublish(blog)
			w.Header().Set("Location", r.URL.String())
		} else if newPage {
			w.Header().Set("Location", r.URL.String()+"/"+strconv.FormatInt(page.Id, 10))
		} else {
			w.Header().Set("Location", r.URL.String())
		}
		w.WriteHeader(303)
		return
	}

	res.tpl = "editpage"

	res.data["page"] = page

	if page.Id != 0 {
		res.data["title"] = page.Title
		res.Output(w, r, page.LastModified())
	} else {
		res.Output(w, r, time.Time{})
	}

}

func PagePreviewHandler(w http.ResponseWriter, r *http.Request) {
	res := NewAuthenticatedResponse(w, r)
	if res == nil {
		return
	}

	values := mux.Vars(r)
	if values["id"] == "" {
		NotFound(w, r)
		return
	}

	page := PageFromQuery(blog, PAGE_TYPE_NONE, FETCH_ALL, "id=?", "", values["id"])
	if page == nil {
		NotFound(w, r)
		return
	}

	res.tpl = "previewpage"
	if page != nil {
		res.data["page"] = page
	}

	res.data["title"] = page.Title

	res.Output(w, r, page.LastModified())
}

func NotFound(w http.ResponseWriter, r *http.Request) {
	res := NewResponse()
	res.errorCode = 404
	res.tpl = "404"
	res.data["url"] = r.URL
	res.Output(w, r, time.Time{})
}
