package main

import (
	"fmt"
	"html/template"
	"strings"
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

func formatMarkdown(text string) template.HTML {
	htmlFlags := blackfriday.HTML_USE_XHTML
	renderer := blackfriday.HtmlRenderer(htmlFlags, "", "")

	extensions := blackfriday.EXTENSION_FENCED_CODE
	return template.HTML(blackfriday.Markdown([]byte(text), renderer, extensions))
}

func isTime(t interface{}) bool {
	if ts, ok := t.(time.Time); ok {
		return !ts.IsZero()
	}
	return false
}

var funcMap = template.FuncMap{
	"capitalize": capitalizeFirst,
	"date":       formatDate,
	"timestamp":  formatTimestamp,
	"markdown":   formatMarkdown,
	"istime":     isTime,
}
