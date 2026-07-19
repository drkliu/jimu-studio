package server

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/drkliu/jimu-studio/internal/auth"
	"github.com/drkliu/jimu-studio/internal/provider"
)

const maxMetadataFormBytes = 256 << 10

var metadataListTemplate = template.Must(template.ParseFS(content, "templates/metadata_list.html"))
var metadataEditTemplate = template.Must(template.ParseFS(content, "templates/metadata_edit.html"))
var metadataConflictTemplate = template.Must(template.ParseFS(content, "templates/metadata_conflict.html"))
var metadataErrorTemplate = template.Must(template.ParseFS(content, "templates/metadata_error.html"))

type metadataEntityView struct {
	Entity  provider.Entity
	EditURL string
}

type metadataListData struct {
	Session    auth.SessionView
	Navigation []navigationItem
	Entities   []metadataEntityView
	Search     string
	NextURL    string
	Updated    string
	CanEdit    bool
}

type metadataEditData struct {
	Session    auth.SessionView
	Navigation []navigationItem
	Entity     provider.Entity
	CanEdit    bool
	CanDelete  bool
	IsCreate   bool
	Message    string
	RequestID  string
}

type metadataConflictData struct {
	Session    auth.SessionView
	Navigation []navigationItem
	Submitted  provider.Entity
	Current    provider.Entity
	RequestID  string
}

type metadataErrorData struct {
	Session    auth.SessionView
	Navigation []navigationItem
	Title      string
	Message    string
	RequestID  string
}

func (server *authenticatedServer) metadataList(response http.ResponseWriter, request *http.Request) {
	sessionID, session, ok := server.authenticatedSession(response, request)
	if !ok {
		return
	}
	cursor := request.URL.Query().Get("cursor")
	search := request.URL.Query().Get("search")
	if !boundedBrowserValue(cursor, 2048) || !boundedBrowserValue(search, 256) {
		http.Error(response, "invalid metadata query", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	var page provider.EntityPage
	err := server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		var requestErr error
		page, requestErr = client.ListEntities(ctx, provider.EntityQuery{Cursor: cursor, Limit: 50, Search: search})
		return requestErr
	})
	if err != nil {
		server.renderMetadataError(response, session, err)
		return
	}
	canEdit := session.HasRole("studio.metadata.write")
	data := metadataListData{Session: session, Navigation: allowedNavigation(session), Search: search, Updated: boundedDisplay(request.URL.Query().Get("updated"), 256), CanEdit: canEdit}
	data.Entities = make([]metadataEntityView, 0, len(page.Items))
	for _, entity := range page.Items {
		view := metadataEntityView{Entity: entity}
		if canEdit {
			view.EditURL = "/metadata/edit?code=" + url.QueryEscape(entity.Code)
		}
		data.Entities = append(data.Entities, view)
	}
	if page.NextCursor != "" {
		values := url.Values{"cursor": {page.NextCursor}}
		if search != "" {
			values.Set("search", search)
		}
		data.NextURL = "/metadata?" + values.Encode()
	}
	renderTemplate(response, http.StatusOK, metadataListTemplate, data)
}

func (server *authenticatedServer) metadataEdit(response http.ResponseWriter, request *http.Request) {
	sessionID, session, ok := server.authenticatedSession(response, request)
	if !ok {
		return
	}
	code := request.URL.Query().Get("code")
	if !boundedBrowserValue(code, 256) || code == "" {
		http.Error(response, "invalid entity code", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	entity, err := server.findEntity(ctx, sessionID, code)
	if err != nil {
		server.renderMetadataError(response, session, err)
		return
	}
	renderTemplate(response, http.StatusOK, metadataEditTemplate, metadataEditData{
		Session: session, Navigation: allowedNavigation(session), Entity: entity, CanEdit: session.HasRole("studio.metadata.write"),
		CanDelete: session.HasRole("studio.metadata.apply"),
	})
}

func (server *authenticatedServer) metadataNew(response http.ResponseWriter, request *http.Request) {
	_, session, ok := server.authenticatedSession(response, request)
	if !ok {
		return
	}
	if !session.HasRole("studio.metadata.write") {
		http.Error(response, "metadata write permission required", http.StatusForbidden)
		return
	}
	renderTemplate(response, http.StatusOK, metadataEditTemplate, metadataEditData{
		Session: session, Navigation: allowedNavigation(session), Entity: provider.Entity{Fields: []provider.Field{{}}},
		CanEdit: true, IsCreate: true,
	})
}

func (server *authenticatedServer) metadataSave(response http.ResponseWriter, request *http.Request) {
	server.metadataMutate(response, request, false)
}

func (server *authenticatedServer) metadataCreate(response http.ResponseWriter, request *http.Request) {
	server.metadataMutate(response, request, true)
}

func (server *authenticatedServer) metadataMutate(response http.ResponseWriter, request *http.Request, create bool) {
	if !sameSiteMutation(request) {
		http.Error(response, "cross-site metadata mutation rejected", http.StatusForbidden)
		return
	}
	sessionID, session, ok := server.authenticatedSession(response, request)
	if !ok {
		return
	}
	if !session.HasRole("studio.metadata.write") {
		http.Error(response, "metadata write permission required", http.StatusForbidden)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxMetadataFormBytes)
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid metadata form", http.StatusBadRequest)
		return
	}
	if err := server.broker.ValidateCSRF(sessionID, request.PostForm.Get("csrf")); err != nil {
		http.Error(response, "metadata mutation rejected", http.StatusForbidden)
		return
	}
	entity, expectedVersion, err := parseMetadataEntity(request.PostForm)
	if err != nil {
		renderTemplate(response, http.StatusBadRequest, metadataEditTemplate, metadataEditData{
			Session: session, Navigation: allowedNavigation(session), Entity: entity, CanEdit: true, IsCreate: create, Message: err.Error(),
		})
		return
	}
	if create && expectedVersion != 0 {
		renderTemplate(response, http.StatusBadRequest, metadataEditTemplate, metadataEditData{
			Session: session, Navigation: allowedNavigation(session), Entity: entity, CanEdit: true, IsCreate: true,
			Message: "A new entity must start at expected version 0.",
		})
		return
	}
	operation := "update"
	if create {
		operation = "create"
	}
	scope := "metadata:" + operation + ":" + entity.Code + ":" + strconv.FormatInt(expectedVersion, 10)
	idempotencyKey, err := server.broker.DraftKey(sessionID, scope)
	if err != nil {
		server.renderMetadataError(response, session, err)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	var updated provider.Entity
	err = server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		var requestErr error
		updated, requestErr = client.PutEntity(ctx, entity, expectedVersion, idempotencyKey)
		return requestErr
	})
	if err == nil {
		server.broker.ClearDraft(sessionID, scope)
		http.Redirect(response, request, "/metadata?updated="+url.QueryEscape(updated.Code), http.StatusSeeOther)
		return
	}
	var apiError *provider.APIError
	if errors.As(err, &apiError) && apiError.Status == http.StatusConflict {
		refreshContext, refreshCancel := context.WithTimeout(request.Context(), 15*time.Second)
		defer refreshCancel()
		current, refreshErr := server.findEntity(refreshContext, sessionID, entity.Code)
		if refreshErr != nil {
			server.renderMetadataError(response, session, refreshErr)
			return
		}
		renderTemplate(response, http.StatusConflict, metadataConflictTemplate, metadataConflictData{
			Session: session, Navigation: allowedNavigation(session), Submitted: entity, Current: current, RequestID: apiError.RequestID,
		})
		return
	}
	if errors.As(err, &apiError) && apiError.Status == http.StatusBadRequest {
		renderTemplate(response, http.StatusBadRequest, metadataEditTemplate, metadataEditData{
			Session: session, Navigation: allowedNavigation(session), Entity: entity, CanEdit: true, IsCreate: create, Message: apiError.Message, RequestID: apiError.RequestID,
		})
		return
	}
	server.renderMetadataError(response, session, err)
}

func (server *authenticatedServer) authenticatedSession(response http.ResponseWriter, request *http.Request) (string, auth.SessionView, bool) {
	sessionID := server.cookieValue(request, "session")
	if sessionID == "" {
		http.Error(response, "authentication required", http.StatusUnauthorized)
		return "", auth.SessionView{}, false
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	session, ok := server.broker.Session(ctx, sessionID)
	if !ok {
		server.clearCookie(response, "session")
		http.Error(response, "authentication required", http.StatusUnauthorized)
		return "", auth.SessionView{}, false
	}
	return sessionID, session, true
}

func (server *authenticatedServer) withProvider(ctx context.Context, sessionID string, operation func(*provider.Client) error) error {
	return server.broker.WithClient(ctx, sessionID, func(client auth.TenantClient) error {
		providerClient, ok := client.(*provider.Client)
		if !ok {
			return errors.New("tenant provider client is incompatible")
		}
		return operation(providerClient)
	})
}

func (server *authenticatedServer) findEntity(ctx context.Context, sessionID, code string) (provider.Entity, error) {
	var page provider.EntityPage
	err := server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		var requestErr error
		page, requestErr = client.ListEntities(ctx, provider.EntityQuery{Limit: 200, Search: code})
		return requestErr
	})
	if err != nil {
		return provider.Entity{}, err
	}
	for _, entity := range page.Items {
		if entity.Code == code {
			return entity, nil
		}
	}
	return provider.Entity{}, &provider.APIError{Status: http.StatusNotFound, Code: "entity_not_found", Message: "Entity was not found."}
}

func parseMetadataEntity(form url.Values) (provider.Entity, int64, error) {
	entity := provider.Entity{Code: form.Get("code"), Name: form.Get("name"), Kind: form.Get("kind")}
	expectedVersion, err := strconv.ParseInt(form.Get("expected_version"), 10, 64)
	if err != nil || expectedVersion < 0 {
		return entity, 0, errors.New("a valid expected version is required")
	}
	entity.Version = expectedVersion
	fieldCount, err := strconv.Atoi(form.Get("field_count"))
	if err != nil || fieldCount < 0 || fieldCount > 200 {
		return entity, expectedVersion, errors.New("field count exceeds the supported bound")
	}
	entity.Fields = make([]provider.Field, 0, fieldCount)
	for index := 0; index < fieldCount; index++ {
		suffix := strconv.Itoa(index)
		entity.Fields = append(entity.Fields, provider.Field{
			Code: form.Get("field_code_" + suffix), DataType: form.Get("field_type_" + suffix),
			Required: form.Has("field_required_" + suffix), ReadOnly: form.Has("field_read_only_" + suffix),
		})
	}
	if !boundedBrowserValue(entity.Code, 256) || entity.Code == "" || !boundedBrowserValue(entity.Name, 512) || entity.Name == "" || !boundedBrowserValue(entity.Kind, 128) {
		return entity, expectedVersion, errors.New("entity values exceed safe bounds")
	}
	for _, field := range entity.Fields {
		if !boundedBrowserValue(field.Code, 256) || field.Code == "" || !boundedBrowserValue(field.DataType, 256) || field.DataType == "" {
			return entity, expectedVersion, errors.New("field values exceed safe bounds")
		}
	}
	return entity, expectedVersion, nil
}

func (server *authenticatedServer) renderMetadataError(response http.ResponseWriter, session auth.SessionView, err error) {
	status := http.StatusBadGateway
	message := "The Jimu provider could not complete this request."
	requestID := ""
	var apiError *provider.APIError
	if errors.As(err, &apiError) {
		if apiError.Status >= 400 && apiError.Status <= 599 {
			status = apiError.Status
		}
		if apiError.Message != "" {
			message = apiError.Message
		}
		requestID = apiError.RequestID
	}
	renderTemplate(response, status, metadataErrorTemplate, metadataErrorData{
		Session: session, Navigation: allowedNavigation(session), Title: http.StatusText(status), Message: message, RequestID: requestID,
	})
}

func boundedBrowserValue(value string, limit int) bool {
	return len(value) <= limit && strings.IndexFunc(value, unicode.IsControl) < 0
}

func boundedDisplay(value string, limit int) string {
	if !boundedBrowserValue(value, limit) {
		return ""
	}
	return value
}

func renderTemplate(response http.ResponseWriter, status int, parsed *template.Template, data any) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	if err := parsed.Execute(response, data); err != nil {
		// Headers may already be committed; never include template data in errors.
		_, _ = response.Write([]byte("Unable to render Studio page."))
	}
}
