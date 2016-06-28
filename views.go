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
)

type Values map[string]string

type Viewer interface {
	Output()
}

type ViewBase struct {
	ctx                 *Context
	data                map[string]interface{}
	tpl                 string
	errorCode           int
	CookieAuthenticated bool
}

func NewView(ctx *Context) *ViewBase {
	var v ViewBase
	v.ctx = ctx
	v.data = make(map[string]interface{}, 3)
	v.data["base"] = v.ctx.URLPrefix
	v.data["siteTitle"] = v.ctx.SiteTitle
	v.data["logo"] = v.ctx.Logo
	v.data["assets"] = v.ctx.URLPrefix + v.ctx.AssetsPrefix
	v.data["admin"] = v.ctx.Origin + v.ctx.URLPrefix + "/admin"
	return &v
}

// Output serves the page, implementing some web server logic. It gives proper
// responses to If-Modified_since requests.
// lastModified is the Last-Modified date of the data added inside the view.
// Output may also add some more and output a different Last-Modified header,
// but if lastModified is nil, no Last-Modified HTTP header will be outputted.
func (v *ViewBase) Output(req *http.Request, w http.ResponseWriter, lastModified time.Time) {

	menu := PagesFromQuery(v.ctx, PAGE_TYPE_STATIC, FETCH_TITLE, "published != 0", "ORDER BY title DESC")
	v.data["menu"] = menu

	h := w.Header()

	if !lastModified.IsZero() {
		// This only adds a Last-Modified header to views that explicitly add a
		// Last-Modified timestamp. Otherwise we might end up with serving old
		// invalid Last-Modified dates.

		lastModified = lastTime(lastModified, v.ctx.GetTemplateModified(v.tpl).Truncate(time.Second), menu.LastModified())

		// These headers must be served at all times, even when sending a 304
		// Not Modified reply.
		// See: http://tools.ietf.org/html/rfc7232#section-4.1
		//     The server generating a 304 response MUST generate any of the
		//     following header fields that would have been sent in a 200 (OK)
		//     response to the same request: Cache-Control, Content-Location, Date,
		//     ETag, Expires, and Vary.

		if v.CookieAuthenticated {
			h.Set("Cache-Control", "private")
			h.Set("Vary", "Cookie")
		} else {
			h.Set("Cache-Control", "max-age=60,s-maxage=5")
		}
	}

	// Test whether we can serve a 304 Not Modified reply.
	if !lastModified.IsZero() && v.errorCode == 0 {
		if equalLastModified(lastModified, req) {
			w.WriteHeader(304) // Not Modified
			return
		}
	}

	tpl := v.ctx.GetTemplate(v.tpl)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)

	err := tpl.Execute(gz, v.data)
	checkError(err, "failed to get view output")

	gz.Close()

	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Encoding", "gzip")
	h.Set("Content-Length", strconv.Itoa(buf.Len()))

	if !lastModified.IsZero() {
		h.Set("Last-Modified", lastModified.UTC().Format(time.RFC1123))
	}

	if v.errorCode != 0 {
		w.WriteHeader(v.errorCode)
	}

	if req.Method != "HEAD" {
		w.Write(buf.Bytes())
	}
}

func OutputStatic(req *http.Request, w http.ResponseWriter, contentType string, p string) {
	f, err := os.Open(p)
	checkError(err, "OutputStatic: could not open")
	st, err := f.Stat()
	checkError(err, "OutputStatic: could not stat file")

	h := w.Header()
	h.Set("Cache-Control", "max-age=3600,s-maxage=5")

	if equalLastModified(st.ModTime(), req) {
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
	h.Set("Last-Modified", st.ModTime().UTC().Format(time.RFC1123))

	if req.Method != "HEAD" {
		n, err := io.Copy(w, f)
		checkError(err, "could not copy file to output")
		if n != st.Size() {
			//raiseError("OutputStatic: size doesn't match with stat output " + strconv.Itoa(int(n)))
		}
	}
}

func AssetHandler(ctx *Context, w http.ResponseWriter, r *http.Request, values Values) {
	name := values["name"]
	if name == "" {
		NotFound(ctx, w, r) // or permission denied?
		return
	}

	ext := path.Ext(name)

	if ext != ".js" && ext != ".css" {
		NotFound(ctx, w, r)
		return
	}

	outpath := path.Join(ctx.WebRoot, ctx.AssetsPrefix, name)
	if _, err := os.Stat(outpath + ".gz"); err == nil {
		// The file already exists. That means the server isn't well-configured
		// or there is a race between creating a file and serving another via
		// the blog.
		OutputStatic(r, w, ext, outpath+".gz")
		return
	} else if !os.IsNotExist(err) {
		checkError(err, "could not stat asset file")
	}

	ctx.loadSkin()
	for i, skin := range ctx.skins {
		var cmd *exec.Cmd // input via command
		var reader io.Reader
		var err error

		// TODO clean up duplicate code

		switch ext {
		case ".js":
			// Only open input JavaScript file
			infile, err := os.Open(path.Join(ctx.BlogPath, "skins", skin, name))
			if os.IsNotExist(err) {
				continue
			}
			checkError(err, "failed to open JavaScript file")
			defer infile.Close()
			reader = infile

		case ".css":
			// Start converting input SCSS file to CSS
			scssPath := path.Join(ctx.BlogPath, "skins", skin, name[:len(name)-len(ext)]+".scss")
			_, err = os.Stat(scssPath)
			if os.IsNotExist(err) {
				continue
			}
			checkError(err, "failed to stat SCSS file")

			args := []string{"--default-encoding", "utf-8", "--no-cache"}
			for j := i; j < len(ctx.skins); j++ {
				args = append(args, "-I", path.Join(ctx.BlogPath, "skins", ctx.skins[j]))
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

		OutputStatic(r, w, ext, outpath+".gz")
	}
}

func BlogIndexHandler(ctx *Context, w http.ResponseWriter, r *http.Request, values Values) {
	v := NewView(ctx)

	v.tpl = "blogindex"

	posts := PagesFromQuery(ctx, PAGE_TYPE_POST, FETCH_TITLE, "published!=0", "ORDER BY published DESC LIMIT 10")
	v.data["posts"] = posts

	v.Output(r, w, posts.LastModified())
}

func PageViewHandler(ctx *Context, w http.ResponseWriter, r *http.Request, values Values) {
	v := NewView(ctx)

	page := PageFromQuery(ctx, PAGE_TYPE_NONE, FETCH_ALL, "name=? AND published!=0", "", values["name"])
	if page == nil {
		NotFound(ctx, w, r)
		return
	}

	switch page.Type {
	case PAGE_TYPE_POST:
		v.tpl = "blogpost"
	case PAGE_TYPE_STATIC:
		v.tpl = "page"
	default:
		raiseError("unknown page type while determining template")
	}

	v.data["page"] = page
	v.data["title"] = page.Title

	v.Output(r, w, page.LastModified())
}

func ArchiveHandler(ctx *Context, w http.ResponseWriter, r *http.Request, values Values) {
	v := NewView(ctx)
	v.tpl = "archive"

	posts := PagesFromQuery(ctx, PAGE_TYPE_POST, FETCH_TITLE, "published!=0", "ORDER BY published DESC")
	v.data["posts"] = posts

	v.Output(r, w, posts.LastModified())
}

func AuthenticatedView(ctx *Context, w http.ResponseWriter, r *http.Request) *ViewBase {
	// Require authenticated views to be of the canonical origin.
	if ctx.OriginURL.Scheme != r.URL.Scheme ||
		ctx.OriginURL.Host != r.URL.Host { // host can also mean host:port
		u := *r.URL
		u.Scheme = ctx.OriginURL.Scheme
		u.Host = ctx.OriginURL.Host
		w.Header().Set("Location", u.String())
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(303)
	}
	user, err := NewUser(ctx, w, r)
	if user == nil {
		view := NewView(ctx)
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
		view.Output(r, w, time.Time{})
		return nil
	}

	view := NewView(ctx)
	view.data["user"] = user
	view.CookieAuthenticated = true

	return view
}

func AdminHandler(ctx *Context, w http.ResponseWriter, r *http.Request, values Values) {
	v := AuthenticatedView(ctx, w, r)
	if v == nil {
		return
	}

	v.tpl = "admin"

	drafts := PagesFromQuery(ctx, PAGE_TYPE_POST, FETCH_TITLE, "published==0", "ORDER BY modified DESC")
	v.data["drafts"] = drafts

	published := PagesFromQuery(ctx, PAGE_TYPE_POST, FETCH_TITLE, "published!=0", "ORDER BY published DESC")
	v.data["published"] = published

	menuUnpublished := PagesFromQuery(v.ctx, PAGE_TYPE_STATIC, FETCH_TITLE, "published == 0", "ORDER BY title DESC")
	v.data["menuUnpublished"] = menuUnpublished

	v.Output(r, w, lastTime(drafts.LastModified(), published.LastModified(), menuUnpublished.LastModified()))
}

func PageEditHandler(ctx *Context, w http.ResponseWriter, r *http.Request, values Values) {
	v := AuthenticatedView(ctx, w, r)
	if v == nil {
		return
	}

	var page *Page

	if values["id"] == "post" {
		page = &Page{Type: PAGE_TYPE_POST}
	} else if values["id"] == "page" {
		page = &Page{Type: PAGE_TYPE_STATIC}
	} else {
		page = PageFromQuery(ctx, PAGE_TYPE_NONE, FETCH_ALL, "id=?", "", values["id"])
		if page == nil {
			NotFound(ctx, w, r)
			return
		}
	}

	if r.Method == "POST" {
		// Page.Id will get set during the update.
		newPage := page.Id == 0

		page.Update(ctx, r.PostFormValue("name"), r.PostFormValue("title"), r.PostFormValue("summary"), r.PostFormValue("text"))

		if r.PostFormValue("publish") != "" {
			page.Publish(ctx)
			w.Header().Set("Location", ctx.URLPrefix+page.Url())
		} else if r.PostFormValue("unpublish") != "" {
			page.Unpublish(ctx)
			w.Header().Set("Location", r.URL.String())
		} else if newPage {
			w.Header().Set("Location", r.URL.String()+"/"+strconv.FormatInt(page.Id, 10))
		} else {
			w.Header().Set("Location", r.URL.String())
		}
		w.WriteHeader(303)
		return
	}

	v.tpl = "editpage"

	v.data["page"] = page

	if page.Id != 0 {
		v.data["title"] = page.Title
		v.Output(r, w, page.LastModified())
	} else {
		v.Output(r, w, time.Time{})
	}

}

func PagePreviewHandler(ctx *Context, w http.ResponseWriter, r *http.Request, values Values) {
	v := AuthenticatedView(ctx, w, r)
	if v == nil {
		return
	}

	if values["id"] == "" {
		NotFound(ctx, w, r)
		return
	}

	page := PageFromQuery(ctx, PAGE_TYPE_NONE, FETCH_ALL, "id=?", "", values["id"])
	if page == nil {
		NotFound(ctx, w, r)
		return
	}

	v.tpl = "previewpage"
	if page != nil {
		v.data["page"] = page
	}

	v.data["title"] = page.Title

	v.Output(r, w, page.LastModified())
}

func NotFound(ctx *Context, w http.ResponseWriter, r *http.Request) {
	v := NewView(ctx)
	v.errorCode = 404
	v.tpl = "404"
	v.data["url"] = r.URL
	v.Output(r, w, time.Time{})
}

func CSRFFailed(ctx *Context, w http.ResponseWriter, r *http.Request) {
	v := NewView(ctx)
	v.errorCode = 403
	v.tpl = "csrf-failed"
	v.data["url"] = r.URL
	v.Output(r, w, time.Time{})
}
