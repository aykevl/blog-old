package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html/template"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/russross/blackfriday"
)

var months = [...]string{
	"januari",
	"februari",
	"maart",
	"april",
	"mei",
	"juni",
	"juli",
	"augustus",
	"september",
	"oktober",
	"november",
	"december",
}

func capitalizeFirst(s string) string {
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func formatDate(date time.Time) string {
	return fmt.Sprintf("%d %s %d", date.Day(), months[date.Month()-1], date.Year())
}

func formatTimestamp(date time.Time) string {
	return date.Format(time.RFC3339)
}

func formatMarkdown(text string) []byte {
	htmlFlags := blackfriday.HTML_USE_XHTML
	renderer := blackfriday.HtmlRenderer(htmlFlags, "", "")

	extensions := 0 | // put all extensions on a separate line
		blackfriday.EXTENSION_FENCED_CODE |
		blackfriday.EXTENSION_TABLES |
		blackfriday.EXTENSION_AUTOLINK |
		blackfriday.EXTENSION_FOOTNOTES
	return blackfriday.Markdown([]byte(text), renderer, extensions)
}

func formatMarkdownText(text string) string {
	return string(formatMarkdown(text))
}

func formatMarkdownHTML(text string) template.HTML {
	return template.HTML(formatMarkdown(text))
}

func isTime(t interface{}) bool {
	if ts, ok := t.(time.Time); ok {
		return !ts.IsZero()
	}
	return false
}

func xmlEscape(s string) string {
	buf := &bytes.Buffer{}
	checkError(xml.EscapeText(buf, []byte(s)), "could not escape XML")
	return string(buf.Bytes())
}

var funcMap = template.FuncMap{
	"capitalize": capitalizeFirst,
	"date":       formatDate,
	"timestamp":  formatTimestamp,
	"markdown":   formatMarkdownHTML,
	"istime":     isTime,
	"xmlescape":  xmlEscape,
}

var funcMapText = texttemplate.FuncMap{
	"timestamp": formatTimestamp,
	"markdown":   formatMarkdownText,
	"xmlescape": xmlEscape,
}
