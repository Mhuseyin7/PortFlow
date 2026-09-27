// Package ui embeds PortFlow's built-in web dashboard. It is served by the
// daemon at http://127.0.0.1:9280/ and is a pragmatic fallback while the
// Tauri desktop app (in ./desktop) is being iterated on.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var static embed.FS

// Handler returns an http.Handler that serves the built-in dashboard.
func Handler() http.Handler {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(sub))
}
