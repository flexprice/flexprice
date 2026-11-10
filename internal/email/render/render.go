// Package render turns an email's view data into its subject, HTML and text parts.
package render

import (
	"bytes"
	"embed"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

//go:embed templates
var files embed.FS

// Content is one rendered email.
type Content struct {
	Subject string
	HTML    string
	Text    string
}

// page pairs an email type's body with the shared layout, once per part.
type page struct {
	html *htmltemplate.Template
	text *texttemplate.Template
}

func newPage(body string) page {
	return page{
		html: htmltemplate.Must(htmltemplate.ParseFS(files, "templates/layout.html", "templates/"+body+".html")),
		text: texttemplate.Must(texttemplate.ParseFS(files, "templates/layout.txt", "templates/"+body+".txt")),
	}
}

func (p page) render(subject string, data any) (Content, error) {
	var html, text bytes.Buffer
	if err := p.html.ExecuteTemplate(&html, "layout", data); err != nil {
		return Content{}, ierr.WithError(err).WithHint("Failed to render email").Mark(ierr.ErrSystem)
	}
	if err := p.text.ExecuteTemplate(&text, "layout", data); err != nil {
		return Content{}, ierr.WithError(err).WithHint("Failed to render email").Mark(ierr.ErrSystem)
	}
	return Content{Subject: headerSafe(subject), HTML: html.String(), Text: text.String()}, nil
}

// headerSafe keeps tenant-supplied text from breaking the Subject header.
func headerSafe(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
