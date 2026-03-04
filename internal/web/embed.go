package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"time"
)

//go:embed all:static
var staticFiles embed.FS

//go:embed all:templates
var templateFiles embed.FS

// funcMap provides helper functions available in all templates.
var funcMap = template.FuncMap{
	"formatTime": func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.Format("2006-01-02 15:04:05")
	},
	"statusClass": func(status string) string {
		switch status {
		case "sent":
			return "status-sent"
		case "failed":
			return "status-failed"
		case "sending":
			return "status-sending"
		case "queued":
			return "status-queued"
		default:
			return ""
		}
	},
	"maskKey": func(key string) string {
		if len(key) <= 8 {
			return key
		}
		return key[:8] + "..." + key[len(key)-4:]
	},
}

// parseTemplates parses all embedded templates with the shared function map.
func parseTemplates() (*template.Template, error) {
	return template.New("").Funcs(funcMap).ParseFS(templateFiles, "templates/*.html", "templates/partials/*.html")
}

// staticFileServer returns an http.Handler that serves the embedded static files.
func staticFileServer() http.Handler {
	sub, _ := fs.Sub(staticFiles, "static")
	return http.FileServer(http.FS(sub))
}
