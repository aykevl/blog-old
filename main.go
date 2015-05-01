package main

import (
	"errors"
	"net/http/cgi"
	"os"
	"path/filepath"

	"bitbucket.org/kardianos/osext"
)

type RequestType int

const (
	REQUEST_TYPE_CGI RequestType = iota + 1
	REQUEST_TYPE_CLI
)

func requestType() RequestType {
	if os.Getenv("REQUEST_METHOD") != "" {
		return REQUEST_TYPE_CGI
	} else {
		return REQUEST_TYPE_CLI
	}
}

func getRoot() (string, error) {
	path, err := osext.ExecutableFolder()
	if err != nil {
		internalError("could not get executable directory", err)
	}

	// just to be sure
	path = filepath.Clean(path)

	if filepath.Base(path) != "bin" {
		return path, errors.New("binary does not reside in a 'bin' directory")
	}

	path = filepath.Dir(path)

	return path, nil
}

func serveCGI(ctx *Context) {
	err := cgi.Serve(ctx.router)
	if err != nil {
		internalError("failed to serve CGI", err)
	}
}

func main() {
	root, err := getRoot()
	if err != nil {
		internalError("failed to get root directory", err)
	}

	ctx := NewContext(root)

	if os.Getenv("REQUEST_METHOD") == "" {
		handleCLI(ctx)
	} else {
		serveCGI(ctx)
	}

	ctx.Close()
}
