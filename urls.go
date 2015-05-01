package main

import (
	"crypto/subtle"
	"net/http"
	"regexp"
	"strings"
)

type RouteHandler func(*Context, http.ResponseWriter, *http.Request, Values)

var Routes = []struct {
	route   string
	handler RouteHandler
}{
	{"", BlogIndexHandler},
	{"(?P<year>[0-9]{4})/(?P<month>(0[0-9]|1[0-2]))/(?P<name>[a-z0-9]+(-[a-z0-9]+)*)", PageViewHandler},
	{"admin/", AdminHandler},
	{"admin/edit/new(?P<id>post|page)", PageEditHandler},
	{"admin/edit/(?P<id>[1-9][0-9]*)", PageEditHandler},
	{"admin/edit/(?P<id>[1-9][0-9]*)/preview", PagePreviewHandler},
	{"archive/", ArchiveHandler},
	{"(?P<name>[a-z0-9]+(-[a-z0-9]+)*)", PageViewHandler},
}

type Route struct {
	*regexp.Regexp
	RouteHandler
}

type Router struct {
	ctx    *Context
	root   string
	routes []Route
}

func NewRouter(ctx *Context) *Router {
	var r Router

	r.ctx = ctx

	r.root = ctx.SiteRoot
	if !strings.HasSuffix(r.root, "/") {
		r.root += "/"
	}

	r.routes = make([]Route, len(Routes))
	for i, route := range Routes {
		r.routes[i] = Route{regexp.MustCompile("^" + route.route + "$"), route.handler}
	}

	return &r
}

// TODO this doesn't belong inside the router
func (r *Router) validateCSRF(req *http.Request) bool {
	if req.Header.Get("Origin") == r.ctx.Origin {
		return true
	}

	csrftokenCookie, err := req.Cookie("csrftoken")
	if err == http.ErrNoCookie {
		return false
	}
	csrftoken := req.PostFormValue("csrftoken")
	// This comparison might not be entirely constant-time due to converting
	// from bytes to strings and back to bytes.
	if len(csrftoken) >= 32 && subtle.ConstantTimeCompare([]byte(csrftoken), []byte(csrftokenCookie.Value)) == 1 {
		return true
	}

	return false
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if !strings.HasPrefix(req.URL.Path, r.root) {
		NotFound(r.ctx, w, req)
		return
	}
	path := req.URL.Path[len(r.root):]

	if req.Method != "GET" && req.Method != "HEAD" {
		if !r.validateCSRF(req) {
			CSRFFailed(r.ctx, w, req)
			return
		}
	}

	for _, route := range r.routes {
		if matches := route.Regexp.FindStringSubmatch(path); matches != nil {
			values := make(map[string]string)
			for i, name := range route.Regexp.SubexpNames() {
				if i == 0 {
					continue
				}
				values[name] = matches[i]
			}
			route.RouteHandler(r.ctx, w, req, values)
			return
		}
	}

	// Page wasn't found. Let's try again with or without ending slash.
	var redirectPath string
	if strings.HasSuffix(path, "/") {
		redirectPath = path[:len(path)-1]
	} else {
		redirectPath = path + "/"
	}

	for _, route := range r.routes {
		if route.Regexp.MatchString(redirectPath) {
			newUrl := *req.URL // copy
			newUrl.Path = r.root + redirectPath
			w.Header().Set("Location", newUrl.String())
			w.WriteHeader(301)
			return
		}
	}

	NotFound(r.ctx, w, req)
}
