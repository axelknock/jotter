package web

import (
	"embed"
	"net/http"
)

//go:embed img
var assets embed.FS

// StaticHandler serves everything under /static/ from embedded web assets
func StaticHandler() http.Handler {
	return http.StripPrefix("/static/", http.FileServer(http.FS(assets)))
}
