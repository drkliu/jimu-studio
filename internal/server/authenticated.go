package server

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/drkliu/jimu-studio/internal/auth"
)

const maxAuthFormBytes = 64 << 10

var authenticatedTemplate = template.Must(template.ParseFS(content, "templates/authenticated.html"))
var authorizationTemplate = template.Must(template.ParseFS(content, "templates/authorize.html"))

type authorizationData struct{ URL string }

type authenticatedServer struct {
	broker        *auth.Broker
	secureCookies bool
}

type navigationItem struct {
	Label string
	Path  string
}

type shellData struct {
	Authenticated bool
	Session       auth.SessionView
	Tenants       []auth.TenantView
	Navigation    []navigationItem
}

// NewAuthenticated constructs the OIDC-backed Studio shell.
func NewAuthenticated(broker *auth.Broker, secureCookies bool) (http.Handler, error) {
	if broker == nil {
		return nil, errors.New("authentication broker is required")
	}
	server := &authenticatedServer{broker: broker, secureCookies: secureCookies}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /", server.index)
	mux.HandleFunc("POST /auth/login", server.login)
	mux.HandleFunc("GET /auth/callback", server.callback)
	mux.HandleFunc("POST /auth/switch", server.switchTenant)
	mux.HandleFunc("POST /auth/logout", server.logout)
	mux.HandleFunc("GET /metadata", server.metadataList)
	mux.HandleFunc("GET /metadata/edit", server.metadataEdit)
	mux.HandleFunc("POST /metadata/edit", server.metadataSave)
	mux.HandleFunc("POST /metadata/plan", server.migrationPlan)
	mux.HandleFunc("POST /metadata/apply", server.migrationApply)
	mux.HandleFunc("GET /identity", server.identityList)
	mux.HandleFunc("POST /identity/mutate", server.identityMutate)
	mux.Handle("GET /assets/", http.FileServerFS(content))
	handler := securityHeaders(mux)
	if secureCookies {
		next := handler
		handler = http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			next.ServeHTTP(response, request)
		})
	}
	return handler, nil
}

func (server *authenticatedServer) index(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(response, request)
		return
	}
	data := shellData{Tenants: server.broker.Tenants()}
	if sessionID := server.cookieValue(request, "session"); sessionID != "" {
		ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
		defer cancel()
		if session, ok := server.broker.Session(ctx, sessionID); ok {
			data.Authenticated = true
			data.Session = session
			data.Navigation = allowedNavigation(session)
		} else {
			server.clearCookie(response, "session")
		}
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := authenticatedTemplate.Execute(response, data); err != nil {
		http.Error(response, "render Studio shell", http.StatusInternalServerError)
	}
}

func (server *authenticatedServer) login(response http.ResponseWriter, request *http.Request) {
	if server.cookieValue(request, "session") != "" {
		http.Error(response, "use tenant switch from the authenticated session", http.StatusConflict)
		return
	}
	if !sameSiteMutation(request) {
		http.Error(response, "cross-site login rejected", http.StatusForbidden)
		return
	}
	if err := parseBoundedForm(response, request); err != nil {
		return
	}
	pending, err := server.broker.Begin(request.PostForm.Get("tenant"))
	if err != nil {
		http.Error(response, "tenant is unavailable", http.StatusBadRequest)
		return
	}
	server.setCookie(response, "pending", pending.ID, 600, http.SameSiteLaxMode)
	server.renderAuthorization(response, pending.URL)
}

func (server *authenticatedServer) callback(response http.ResponseWriter, request *http.Request) {
	pendingID := server.cookieValue(request, "pending")
	server.clearCookie(response, "pending")
	if pendingID == "" || request.URL.Query().Get("error") != "" {
		http.Error(response, "authentication failed", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	session, err := server.broker.Complete(ctx, pendingID, request.URL.Query().Get("state"), request.URL.Query().Get("code"))
	if err != nil {
		http.Error(response, "authentication failed", http.StatusUnauthorized)
		return
	}
	server.setCookie(response, "session", session.ID, 8*60*60, http.SameSiteStrictMode)
	http.Redirect(response, request, "/", http.StatusSeeOther)
}

func (server *authenticatedServer) switchTenant(response http.ResponseWriter, request *http.Request) {
	if !sameSiteMutation(request) {
		http.Error(response, "cross-site switch rejected", http.StatusForbidden)
		return
	}
	if err := parseBoundedForm(response, request); err != nil {
		return
	}
	sessionID := server.cookieValue(request, "session")
	pending, err := server.broker.Switch(sessionID, request.PostForm.Get("csrf"), request.PostForm.Get("tenant"))
	if err != nil {
		status := http.StatusForbidden
		if errors.Is(err, auth.ErrUnknownTenant) {
			status = http.StatusBadRequest
		}
		http.Error(response, "tenant switch rejected", status)
		return
	}
	server.clearCookie(response, "session")
	server.setCookie(response, "pending", pending.ID, 600, http.SameSiteLaxMode)
	server.renderAuthorization(response, pending.URL)
}

func (server *authenticatedServer) logout(response http.ResponseWriter, request *http.Request) {
	if !sameSiteMutation(request) {
		http.Error(response, "cross-site logout rejected", http.StatusForbidden)
		return
	}
	if err := parseBoundedForm(response, request); err != nil {
		return
	}
	if err := server.broker.Logout(server.cookieValue(request, "session"), request.PostForm.Get("csrf")); err != nil {
		http.Error(response, "logout rejected", http.StatusForbidden)
		return
	}
	server.clearCookie(response, "session")
	http.Redirect(response, request, "/", http.StatusSeeOther)
}

func allowedNavigation(session auth.SessionView) []navigationItem {
	definitions := []struct {
		role  string
		label string
		path  string
	}{
		{role: "studio.metadata.read", label: "Metadata", path: "/metadata"},
		{role: "identity.admin", label: "Users and roles", path: "/identity"},
		{role: "studio.workflow.read", label: "Workflows", path: "/workflows"},
		{role: "studio.quota.read", label: "Quota and usage", path: "/quota"},
		{role: "studio.audit.read", label: "Audit", path: "/audit"},
	}
	items := make([]navigationItem, 0, len(definitions))
	for _, definition := range definitions {
		if session.HasRole(definition.role) {
			items = append(items, navigationItem{Label: definition.label, Path: definition.path})
		}
	}
	return items
}

func (server *authenticatedServer) renderAuthorization(response http.ResponseWriter, authorizationURL string) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := authorizationTemplate.Execute(response, authorizationData{URL: authorizationURL}); err != nil {
		http.Error(response, "render authorization continuation", http.StatusInternalServerError)
	}
}

func parseBoundedForm(response http.ResponseWriter, request *http.Request) error {
	request.Body = http.MaxBytesReader(response, request.Body, maxAuthFormBytes)
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid form", http.StatusBadRequest)
		return err
	}
	return nil
}

func sameSiteMutation(request *http.Request) bool {
	return !strings.EqualFold(request.Header.Get("Sec-Fetch-Site"), "cross-site")
}

func (server *authenticatedServer) cookieName(kind string) string {
	if server.secureCookies {
		return "__Host-studio_" + kind
	}
	return "studio_" + kind
}

func (server *authenticatedServer) cookieValue(request *http.Request, kind string) string {
	cookie, err := request.Cookie(server.cookieName(kind))
	if err != nil || len(cookie.Value) > 256 {
		return ""
	}
	return cookie.Value
}

func (server *authenticatedServer) setCookie(response http.ResponseWriter, kind, value string, maxAge int, sameSite http.SameSite) {
	http.SetCookie(response, &http.Cookie{
		Name:     server.cookieName(kind),
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   server.secureCookies,
		SameSite: sameSite,
	})
}

func (server *authenticatedServer) clearCookie(response http.ResponseWriter, kind string) {
	server.setCookie(response, kind, "", -1, http.SameSiteStrictMode)
}
