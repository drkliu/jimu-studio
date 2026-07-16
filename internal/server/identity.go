package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/drkliu/jimu-studio/internal/auth"
	"github.com/drkliu/jimu-studio/internal/provider"
)

var identityTemplate = template.Must(template.ParseFS(content, "templates/identity.html"))

type identityData struct {
	Session      auth.SessionView
	Navigation   []navigationItem
	Users        []provider.User
	Roles        []provider.Role
	UserSearch   string
	RoleSearch   string
	NextUsers    string
	NextRoles    string
	Message      string
	RequestID    string
	Confirmation string
}

func (server *authenticatedServer) identityList(response http.ResponseWriter, request *http.Request) {
	sessionID, session, ok := server.authenticatedSession(response, request)
	if !ok {
		return
	}
	if !session.HasRole("identity.admin") {
		http.Error(response, "identity administrator permission required", http.StatusForbidden)
		return
	}
	server.renderIdentity(response, request, sessionID, session, http.StatusOK, "", "")
}

func (server *authenticatedServer) renderIdentity(response http.ResponseWriter, request *http.Request, sessionID string, session auth.SessionView, status int, message, requestID string) {
	query := request.URL.Query()
	userCursor, userSearch := query.Get("user_cursor"), query.Get("user_search")
	roleCursor, roleSearch := query.Get("role_cursor"), query.Get("role_search")
	for _, value := range []struct {
		v string
		n int
	}{{userCursor, 2048}, {roleCursor, 2048}, {userSearch, 256}, {roleSearch, 256}} {
		if !boundedBrowserValue(value.v, value.n) {
			http.Error(response, "invalid identity query", http.StatusBadRequest)
			return
		}
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	var users provider.UserPage
	var roles provider.RolePage
	err := server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		var e error
		users, e = client.ListUsers(ctx, provider.IdentityQuery{Cursor: userCursor, Limit: 50, Search: userSearch})
		if e != nil {
			return e
		}
		roles, e = client.ListRoles(ctx, provider.IdentityQuery{Cursor: roleCursor, Limit: 50, Search: roleSearch})
		return e
	})
	if err != nil {
		server.renderMetadataError(response, session, err)
		return
	}
	data := identityData{Session: session, Navigation: allowedNavigation(session), Users: users.Items, Roles: roles.Items, UserSearch: userSearch, RoleSearch: roleSearch, Message: message, RequestID: requestID, Confirmation: provider.DisableUserConfirmation}
	if users.NextCursor != "" {
		values := url.Values{"user_cursor": {users.NextCursor}}
		if userSearch != "" {
			values.Set("user_search", userSearch)
		}
		if roleSearch != "" {
			values.Set("role_search", roleSearch)
		}
		data.NextUsers = "/identity?" + values.Encode()
	}
	if roles.NextCursor != "" {
		values := url.Values{"role_cursor": {roles.NextCursor}}
		if roleSearch != "" {
			values.Set("role_search", roleSearch)
		}
		if userSearch != "" {
			values.Set("user_search", userSearch)
		}
		data.NextRoles = "/identity?" + values.Encode()
	}
	renderTemplate(response, status, identityTemplate, data)
}

func (server *authenticatedServer) identityMutate(response http.ResponseWriter, request *http.Request) {
	if !sameSiteMutation(request) {
		http.Error(response, "cross-site identity mutation rejected", http.StatusForbidden)
		return
	}
	sessionID, session, ok := server.authenticatedSession(response, request)
	if !ok {
		return
	}
	if !session.HasRole("identity.admin") {
		http.Error(response, "identity administrator permission required", http.StatusForbidden)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxMetadataFormBytes)
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid identity form", http.StatusBadRequest)
		return
	}
	if err := server.broker.ValidateCSRF(sessionID, request.PostForm.Get("csrf")); err != nil {
		http.Error(response, "identity mutation rejected", http.StatusForbidden)
		return
	}
	action := request.PostForm.Get("action")
	if !boundedBrowserValue(action, 64) || action == "" {
		http.Error(response, "identity action required", http.StatusBadRequest)
		return
	}
	if action == "user-status" && request.PostForm.Get("confirmation") != provider.DisableUserConfirmation {
		server.renderIdentity(response, request, sessionID, session, http.StatusBadRequest, "Type the required confirmation exactly.", "")
		return
	}
	version, versionErr := parseIdentityVersion(request.PostForm.Get("expected_version"))
	if action != "user-create" && action != "role-create" && versionErr != nil {
		http.Error(response, "valid expected version required", http.StatusBadRequest)
		return
	}
	if action == "role-update" {
		role, err := server.findIdentityRole(request.Context(), sessionID, request.PostForm.Get("role_key"))
		if err != nil {
			server.renderMetadataError(response, session, err)
			return
		}
		if role.System {
			server.renderIdentity(response, request, sessionID, session, http.StatusConflict, "System roles are immutable.", "")
			return
		}
	}
	scope := "identity:" + action + ":" + identityBasis(request.PostForm)
	key, err := server.broker.DraftKey(sessionID, scope)
	if err != nil {
		server.renderMetadataError(response, session, err)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	err = server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		switch action {
		case "user-create":
			_, err = client.CreateUser(ctx, provider.User{ID: request.PostForm.Get("user_id"), DisplayName: request.PostForm.Get("display_name"), Email: request.PostForm.Get("email")}, key)
		case "user-status":
			_, err = client.SetUserStatus(ctx, request.PostForm.Get("user_id"), request.PostForm.Get("status"), version, key)
		case "role-create":
			_, err = client.CreateRole(ctx, provider.Role{Key: request.PostForm.Get("role_key"), DisplayName: request.PostForm.Get("display_name")}, key)
		case "role-update":
			_, err = client.UpdateRole(ctx, request.PostForm.Get("role_key"), request.PostForm.Get("display_name"), version, key)
		case "role-assign":
			_, err = client.ChangeUserRole(ctx, request.PostForm.Get("user_id"), request.PostForm.Get("role_key"), version, key, true)
		case "role-revoke":
			_, err = client.ChangeUserRole(ctx, request.PostForm.Get("user_id"), request.PostForm.Get("role_key"), version, key, false)
		default:
			return errors.New("unsupported identity action")
		}
		return err
	})
	if err != nil {
		var api *provider.APIError
		if errors.As(err, &api) {
			msg := api.Message
			if msg == "" {
				msg = "The provider rejected the identity operation."
			}
			server.renderIdentity(response, request, sessionID, session, api.Status, msg, api.RequestID)
			return
		}
		server.renderMetadataError(response, session, err)
		return
	}
	server.broker.ClearDraft(sessionID, scope)
	http.Redirect(response, request, "/identity?updated="+url.QueryEscape(action), http.StatusSeeOther)
}

func (server *authenticatedServer) findIdentityRole(parent context.Context, sessionID, key string) (provider.Role, error) {
	if !boundedBrowserValue(key, 256) || key == "" {
		return provider.Role{}, errors.New("safe role key required")
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	var page provider.RolePage
	err := server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		var e error
		page, e = client.ListRoles(ctx, provider.IdentityQuery{Limit: 200, Search: key})
		return e
	})
	if err != nil {
		return provider.Role{}, err
	}
	for _, role := range page.Items {
		if role.Key == key {
			return role, nil
		}
	}
	return provider.Role{}, &provider.APIError{Status: http.StatusNotFound, Code: "role_not_found", Message: "Role was not found."}
}
func parseIdentityVersion(value string) (int64, error) {
	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil || version < 0 {
		return 0, errors.New("invalid version")
	}
	return version, nil
}
func identityBasis(form url.Values) string {
	copy := url.Values{}
	for key, values := range form {
		if key != "csrf" {
			copy[key] = append([]string(nil), values...)
		}
	}
	sum := sha256.Sum256([]byte(copy.Encode()))
	return hex.EncodeToString(sum[:])
}
