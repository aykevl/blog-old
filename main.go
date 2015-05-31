package main // import "github.com/aykevl/blog"

import (
	"errors"
	"net/http/cgi"
	"os"
	"path/filepath"

	"github.com/kardianos/osext"
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
	checkError(err, "could not get executable directory")

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
	checkError(err, "failed to serve CGI")
}

func main() {
	root, err := getRoot()
	checkError(err, "failed to get root directory")

	ctx := NewContext(root)
	defer ctx.Close()

	if os.Getenv("REQUEST_METHOD") != "" {
		serveCGI(ctx)
	} else {
		// includes FastCGI
		handleCLI(ctx)
	}
}
