// Package ui serves the probe's built-in dashboard: one embedded page that
// reads /status and /history, so an operator can see how the suite is doing
// without any tooling beyond a port-forward.
package ui

import (
	"bytes"
	_ "embed"
	"net/http"
	"time"
)

//go:embed index.html
var index []byte

// Handler serves the dashboard at / and /ui; every other path is not found.
func Handler() http.Handler {
	mux := http.NewServeMux()
	serve := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	}
	mux.HandleFunc("GET /{$}", serve)
	mux.HandleFunc("GET /ui", serve)
	return mux
}
