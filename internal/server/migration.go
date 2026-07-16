package server

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/drkliu/jimu-studio/internal/auth"
	"github.com/drkliu/jimu-studio/internal/provider"
)

const migrationDraftCategory = "migration"

var migrationReviewTemplate = template.Must(template.ParseFS(content, "templates/migration_review.html"))
var migrationAppliedTemplate = template.Must(template.ParseFS(content, "templates/migration_applied.html"))

type migrationDraft struct {
	Entities  []provider.Entity
	Plans     []provider.MigrationPlan
	PlanScope string
}

type migrationReviewData struct {
	Session      auth.SessionView
	Navigation   []navigationItem
	Plans        []provider.MigrationPlan
	Draft        string
	CanApply     bool
	Confirmation string
	Message      string
}

type migrationAppliedData struct {
	Session    auth.SessionView
	Navigation []navigationItem
	Plans      []provider.MigrationPlan
}

func (server *authenticatedServer) migrationPlan(response http.ResponseWriter, request *http.Request) {
	if !sameSiteMutation(request) {
		http.Error(response, "cross-site migration plan rejected", http.StatusForbidden)
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
		http.Error(response, "invalid migration plan form", http.StatusBadRequest)
		return
	}
	if err := server.broker.ValidateCSRF(sessionID, request.PostForm.Get("csrf")); err != nil {
		http.Error(response, "migration plan rejected", http.StatusForbidden)
		return
	}
	code := request.PostForm.Get("code")
	if code == "" || !boundedBrowserValue(code, 256) {
		http.Error(response, "invalid entity code", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	entity, err := server.findEntity(ctx, sessionID, code)
	if err != nil {
		server.renderMigrationError(response, session, err)
		return
	}
	planScope := "migration-plan:" + entity.Code + ":" + strconv.FormatInt(entity.Version, 10)
	idempotencyKey, err := server.broker.DraftKey(sessionID, planScope)
	if err != nil {
		server.renderMigrationError(response, session, err)
		return
	}
	var plans []provider.MigrationPlan
	err = server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		var requestErr error
		plans, requestErr = client.PlanMigrations(ctx, []provider.Entity{entity}, idempotencyKey)
		return requestErr
	})
	if err != nil {
		server.renderMigrationError(response, session, err)
		return
	}
	draft := migrationDraft{Entities: cloneEntities([]provider.Entity{entity}), Plans: append([]provider.MigrationPlan(nil), plans...), PlanScope: planScope}
	reference, err := server.broker.StoreDraft(sessionID, migrationDraftCategory, draft)
	if err != nil {
		server.renderMigrationError(response, session, err)
		return
	}
	renderTemplate(response, http.StatusOK, migrationReviewTemplate, migrationReviewData{
		Session: session, Navigation: allowedNavigation(session), Plans: plans, Draft: reference,
		CanApply: len(plans) > 0 && session.HasRole("studio.metadata.apply"), Confirmation: provider.ApplyMigrationConfirmation,
	})
}

func (server *authenticatedServer) migrationApply(response http.ResponseWriter, request *http.Request) {
	if !sameSiteMutation(request) {
		http.Error(response, "cross-site migration apply rejected", http.StatusForbidden)
		return
	}
	sessionID, session, ok := server.authenticatedSession(response, request)
	if !ok {
		return
	}
	if !session.HasRole("studio.metadata.apply") {
		http.Error(response, "migration apply permission required", http.StatusForbidden)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxMetadataFormBytes)
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid migration apply form", http.StatusBadRequest)
		return
	}
	if err := server.broker.ValidateCSRF(sessionID, request.PostForm.Get("csrf")); err != nil {
		http.Error(response, "migration apply rejected", http.StatusForbidden)
		return
	}
	reference := request.PostForm.Get("draft")
	if !boundedBrowserValue(reference, 256) || reference == "" {
		http.Error(response, "migration plan reference required", http.StatusBadRequest)
		return
	}
	value, found := server.broker.LoadDraft(sessionID, migrationDraftCategory, reference)
	draft, compatible := value.(migrationDraft)
	if !found || !compatible || len(draft.Entities) == 0 || len(draft.Plans) == 0 {
		renderTemplate(response, http.StatusConflict, metadataErrorTemplate, metadataErrorData{
			Session: session, Navigation: allowedNavigation(session), Title: "Migration plan expired", Message: "Create and review a fresh migration plan before applying.",
		})
		return
	}
	if request.PostForm.Get("confirmation") != provider.ApplyMigrationConfirmation {
		renderTemplate(response, http.StatusBadRequest, migrationReviewTemplate, migrationReviewData{
			Session: session, Navigation: allowedNavigation(session), Plans: draft.Plans, Draft: reference,
			Message:  "Type the required confirmation exactly before applying.",
			CanApply: true, Confirmation: provider.ApplyMigrationConfirmation,
		})
		return
	}
	applyScope := "migration-apply:" + reference
	idempotencyKey, err := server.broker.DraftKey(sessionID, applyScope)
	if err != nil {
		server.renderMigrationError(response, session, err)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()
	var applied []provider.MigrationPlan
	err = server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		var requestErr error
		applied, requestErr = client.ApplyMigrations(ctx, cloneEntities(draft.Entities), idempotencyKey)
		return requestErr
	})
	if err != nil {
		var apiError *provider.APIError
		if errors.As(err, &apiError) && apiError.Status == http.StatusConflict {
			server.broker.DeleteDraft(sessionID, migrationDraftCategory, reference)
			server.broker.ClearDraft(sessionID, draft.PlanScope)
			server.broker.ClearDraft(sessionID, applyScope)
			renderTemplate(response, http.StatusConflict, metadataErrorTemplate, metadataErrorData{
				Session: session, Navigation: allowedNavigation(session), Title: "Migration plan is stale",
				Message: "Metadata changed after planning. Nothing was applied; create and review a fresh plan.", RequestID: apiError.RequestID,
			})
			return
		}
		server.renderMigrationError(response, session, err)
		return
	}
	server.broker.DeleteDraft(sessionID, migrationDraftCategory, reference)
	server.broker.ClearDraft(sessionID, draft.PlanScope)
	server.broker.ClearDraft(sessionID, applyScope)
	renderTemplate(response, http.StatusOK, migrationAppliedTemplate, migrationAppliedData{
		Session: session, Navigation: allowedNavigation(session), Plans: applied,
	})
}

func cloneEntities(entities []provider.Entity) []provider.Entity {
	cloned := make([]provider.Entity, len(entities))
	for index, entity := range entities {
		cloned[index] = entity
		cloned[index].Fields = append([]provider.Field(nil), entity.Fields...)
	}
	return cloned
}
