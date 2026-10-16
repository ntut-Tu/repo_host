// Package api embeds the service's public HTTP contract and route browser.
package api

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.json
var specification []byte

//go:embed docs.html
var documentation []byte

func Spec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(specification)
}

func Docs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'")
	_, _ = w.Write(documentation)
}
