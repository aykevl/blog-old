package main

import (
	"fmt"
	"html/template"
	"os"
)

var error500Template *template.Template

const error500TemplateText = "<h1>500 Internal Server Error</h1><p>{{.Reason}}: {{.Error}}</p>\n"

func internalError(reason interface{}, err error) {
	switch requestType() {
	case REQUEST_TYPE_CGI:
		fmt.Println("Status: 500")
		fmt.Println("Content-Type: text/html; charset=utf-8\n")
		error500Template.Execute(os.Stdout, map[string]interface{}{"Reason": reason, "Error": err})
	case REQUEST_TYPE_CLI:
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s\n", reason, err)
		} else {
			fmt.Fprintln(os.Stderr, reason)
		}
	default:
		panic("unknown request type")
	}
	os.Exit(1)
}

func checkError(err error, reason interface{}) {
	if err != nil {
		internalError(reason, err)
	}
}

// raiseError throws an error without needing an error type
func raiseError(reason interface{}) {
	internalError(reason, nil)
}

func init() {
	error500Template = template.Must(template.New("").Parse(error500TemplateText))
}
