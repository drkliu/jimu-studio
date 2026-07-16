// Package server provides the Jimu Studio HTTP application.
package server

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*.html assets/*
var content embed.FS

var indexTemplate = template.Must(template.ParseFS(content, "templates/index.html"))

// New constructs the Studio handler without starting network listeners.
func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /", index)
	mux.Handle("GET /assets/", http.FileServerFS(content))
	return securityHeaders(mux)
}

func health(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	_, _ = response.Write([]byte("{\"status\":\"ok\"}\n"))
}

func index(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(response, request)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTemplate.Execute(response, nil); err != nil {
		http.Error(response, "render Studio shell", http.StatusInternalServerError)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; object-src 'none'")
		response.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		response.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(response, request)
	})
}
