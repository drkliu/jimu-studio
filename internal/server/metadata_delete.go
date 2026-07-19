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

const metadataDeletionDraftCategory = "metadata-delete"

var metadataDeleteReviewTemplate = template.Must(template.ParseFS(content, "templates/metadata_delete_review.html"))
var metadataDeletedTemplate = template.Must(template.ParseFS(content, "templates/metadata_deleted.html"))

type metadataDeletionDraft struct {
	Plan      provider.DeletionPlan
	PlanScope string
}

type metadataDeleteReviewData struct {
	Session      auth.SessionView
	Navigation   []navigationItem
	Plan         provider.DeletionPlan
	Draft        string
	Confirmation string
	Message      string
}

type metadataDeletedData struct {
	Session    auth.SessionView
	Navigation []navigationItem
	Receipt    provider.DeletionReceipt
}

func (server *authenticatedServer) metadataDeletePlan(response http.ResponseWriter, request *http.Request) {
	if !sameSiteMutation(request) {
		http.Error(response, "cross-site metadata deletion plan rejected", http.StatusForbidden)
		return
	}
	sessionID, session, ok := server.authenticatedSession(response, request)
	if !ok {
		return
	}
	if !session.HasRole("studio.metadata.apply") {
		http.Error(response, "metadata deletion permission required", http.StatusForbidden)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxMetadataFormBytes)
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid metadata deletion form", http.StatusBadRequest)
		return
	}
	if err := server.broker.ValidateCSRF(sessionID, request.PostForm.Get("csrf")); err != nil {
		http.Error(response, "metadata deletion plan rejected", http.StatusForbidden)
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
		server.renderMetadataError(response, session, err)
		return
	}
	planScope := "metadata-delete-plan:" + entity.Code + ":" + strconv.FormatInt(entity.Version, 10)
	idempotencyKey, err := server.broker.DraftKey(sessionID, planScope)
	if err != nil {
		server.renderMetadataError(response, session, err)
		return
	}
	var plan provider.DeletionPlan
	err = server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		var requestErr error
		plan, requestErr = client.PlanEntityDeletion(ctx, entity.Code, entity.Version, idempotencyKey)
		return requestErr
	})
	if err != nil {
		server.renderMetadataError(response, session, err)
		return
	}
	if !plan.Deletable {
		server.broker.ClearDraft(sessionID, planScope)
		renderTemplate(response, http.StatusOK, metadataDeleteReviewTemplate, metadataDeleteReviewData{
			Session: session, Navigation: allowedNavigation(session), Plan: plan, Confirmation: provider.DeleteEntityConfirmation,
		})
		return
	}
	reference, err := server.broker.StoreDraft(sessionID, metadataDeletionDraftCategory, metadataDeletionDraft{Plan: plan, PlanScope: planScope})
	if err != nil {
		server.renderMetadataError(response, session, err)
		return
	}
	renderTemplate(response, http.StatusOK, metadataDeleteReviewTemplate, metadataDeleteReviewData{
		Session: session, Navigation: allowedNavigation(session), Plan: plan, Draft: reference, Confirmation: provider.DeleteEntityConfirmation,
	})
}

func (server *authenticatedServer) metadataDeleteApply(response http.ResponseWriter, request *http.Request) {
	if !sameSiteMutation(request) {
		http.Error(response, "cross-site metadata deletion rejected", http.StatusForbidden)
		return
	}
	sessionID, session, ok := server.authenticatedSession(response, request)
	if !ok {
		return
	}
	if !session.HasRole("studio.metadata.apply") {
		http.Error(response, "metadata deletion permission required", http.StatusForbidden)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxMetadataFormBytes)
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid metadata deletion form", http.StatusBadRequest)
		return
	}
	if err := server.broker.ValidateCSRF(sessionID, request.PostForm.Get("csrf")); err != nil {
		http.Error(response, "metadata deletion rejected", http.StatusForbidden)
		return
	}
	reference := request.PostForm.Get("draft")
	if reference == "" || !boundedBrowserValue(reference, 256) {
		http.Error(response, "metadata deletion plan reference required", http.StatusBadRequest)
		return
	}
	value, found := server.broker.LoadDraft(sessionID, metadataDeletionDraftCategory, reference)
	draft, compatible := value.(metadataDeletionDraft)
	if !found || !compatible {
		server.renderExpiredDeletion(response, session)
		return
	}
	if !draft.Plan.Deletable {
		renderTemplate(response, http.StatusConflict, metadataDeleteReviewTemplate, metadataDeleteReviewData{
			Session: session, Navigation: allowedNavigation(session), Plan: draft.Plan, Draft: reference,
			Confirmation: provider.DeleteEntityConfirmation, Message: "The provider reports dependencies. Resolve them and create a fresh deletion plan.",
		})
		return
	}
	if request.PostForm.Get("confirmation") != provider.DeleteEntityConfirmation {
		renderTemplate(response, http.StatusBadRequest, metadataDeleteReviewTemplate, metadataDeleteReviewData{
			Session: session, Navigation: allowedNavigation(session), Plan: draft.Plan, Draft: reference,
			Confirmation: provider.DeleteEntityConfirmation, Message: "Type the required confirmation exactly before deleting.",
		})
		return
	}
	applyScope := "metadata-delete-apply:" + reference
	idempotencyKey, err := server.broker.DraftKey(sessionID, applyScope)
	if err != nil {
		server.renderMetadataError(response, session, err)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	var receipt provider.DeletionReceipt
	err = server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		var requestErr error
		receipt, requestErr = client.DeleteEntity(ctx, draft.Plan.Code, draft.Plan.ExpectedVersion, idempotencyKey)
		return requestErr
	})
	if err != nil {
		var apiError *provider.APIError
		if errors.As(err, &apiError) && apiError.Status == http.StatusConflict {
			server.clearMetadataDeletionDraft(sessionID, reference, draft, applyScope)
			server.renderExpiredDeletion(response, session)
			return
		}
		server.renderMetadataError(response, session, err)
		return
	}
	server.clearMetadataDeletionDraft(sessionID, reference, draft, applyScope)
	renderTemplate(response, http.StatusOK, metadataDeletedTemplate, metadataDeletedData{
		Session: session, Navigation: allowedNavigation(session), Receipt: receipt,
	})
}

func (server *authenticatedServer) clearMetadataDeletionDraft(sessionID, reference string, draft metadataDeletionDraft, applyScope string) {
	server.broker.DeleteDraft(sessionID, metadataDeletionDraftCategory, reference)
	server.broker.ClearDraft(sessionID, draft.PlanScope)
	server.broker.ClearDraft(sessionID, applyScope)
}

func (server *authenticatedServer) renderExpiredDeletion(response http.ResponseWriter, session auth.SessionView) {
	renderTemplate(response, http.StatusConflict, metadataErrorTemplate, metadataErrorData{
		Session: session, Navigation: allowedNavigation(session), Title: "Deletion plan expired",
		Message: "Metadata changed or the plan was already used. Nothing was deleted; create and review a fresh plan.",
	})
}
