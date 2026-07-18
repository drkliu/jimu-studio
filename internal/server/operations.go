package server

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/drkliu/jimu-studio/internal/auth"
	"github.com/drkliu/jimu-studio/internal/provider"
)

var workflowTemplate = template.Must(template.ParseFS(content, "templates/workflows.html"))
var quotaTemplate = template.Must(template.ParseFS(content, "templates/quota.html"))
var auditTemplate = template.Must(template.ParseFS(content, "templates/audit.html"))

type workflowData struct {
	Session      auth.SessionView
	Navigation   []navigationItem
	Runs         []provider.WorkflowRun
	Tasks        []provider.WorkflowTask
	SelectedRun  string
	Search       string
	NextRuns     string
	NextTasks    string
	Message      string
	RequestID    string
	CanOperate   bool
	CancelPhrase string
	RetryPhrase  string
}

type quotaData struct {
	Session       auth.SessionView
	Navigation    []navigationItem
	Plans         []provider.QuotaPlan
	Usage         []provider.UsageRecord
	Discrepancies []provider.QuotaDiscrepancy
	Search        string
	NextPlans     string
	NextUsage     string
	NextDiffs     string
	Message       string
	RequestID     string
	CanAdmin      bool
	Confirmation  string
}

type auditData struct {
	Session    auth.SessionView
	Navigation []navigationItem
	Items      []provider.AuditRecord
	Search     string
	Next       string
}

func (server *authenticatedServer) workflowList(response http.ResponseWriter, request *http.Request) {
	sessionID, session, ok := server.authenticatedSession(response, request)
	if !ok {
		return
	}
	if !session.HasRole("studio.workflow.read") {
		http.Error(response, "workflow read permission required", http.StatusForbidden)
		return
	}
	server.renderWorkflows(response, request, sessionID, session, http.StatusOK, "", "")
}

func (server *authenticatedServer) renderWorkflows(response http.ResponseWriter, request *http.Request, sessionID string, session auth.SessionView, status int, message, requestID string) {
	query := request.URL.Query()
	search, runCursor, taskCursor, selected := query.Get("search"), query.Get("run_cursor"), query.Get("task_cursor"), query.Get("run")
	if !boundedOperationsQuery(search, runCursor, taskCursor, selected) {
		http.Error(response, "invalid workflow query", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	var runs provider.WorkflowRunPage
	var tasks provider.WorkflowTaskPage
	err := server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		var listErr error
		runs, listErr = client.ListWorkflowRuns(ctx, provider.OperationsQuery{Cursor: runCursor, Limit: 50, Search: search})
		if listErr != nil {
			return listErr
		}
		if selected == "" && len(runs.Items) != 0 {
			selected = runs.Items[0].ID
		}
		if selected != "" {
			tasks, listErr = client.ListWorkflowTasks(ctx, selected, provider.OperationsQuery{Cursor: taskCursor, Limit: 50})
		}
		return listErr
	})
	if err != nil {
		server.renderMetadataError(response, session, err)
		return
	}
	data := workflowData{Session: session, Navigation: allowedNavigation(session), Runs: runs.Items, Tasks: tasks.Items, SelectedRun: selected, Search: search, Message: message, RequestID: requestID, CanOperate: session.HasRole("studio.workflow.operate"), CancelPhrase: provider.CancelRunConfirmation, RetryPhrase: provider.RetryTaskConfirmation}
	if runs.NextCursor != "" {
		data.NextRuns = operationPageURL("/workflows", url.Values{"run_cursor": {runs.NextCursor}, "search": {search}, "run": {selected}})
	}
	if tasks.NextCursor != "" {
		data.NextTasks = operationPageURL("/workflows", url.Values{"task_cursor": {tasks.NextCursor}, "search": {search}, "run": {selected}})
	}
	renderTemplate(response, status, workflowTemplate, data)
}

func (server *authenticatedServer) workflowMutate(response http.ResponseWriter, request *http.Request) {
	if !sameSiteMutation(request) {
		http.Error(response, "cross-site workflow mutation rejected", http.StatusForbidden)
		return
	}
	sessionID, session, ok := server.authenticatedSession(response, request)
	if !ok {
		return
	}
	if !session.HasRole("studio.workflow.operate") {
		http.Error(response, "workflow operate permission required", http.StatusForbidden)
		return
	}
	if !parseOperationForm(response, request) || server.broker.ValidateCSRF(sessionID, request.PostForm.Get("csrf")) != nil {
		http.Error(response, "workflow mutation rejected", http.StatusForbidden)
		return
	}
	action, id := request.PostForm.Get("action"), request.PostForm.Get("id")
	version, err := parseIdentityVersion(request.PostForm.Get("expected_version"))
	if err != nil || !boundedBrowserValue(id, 256) || id == "" {
		http.Error(response, "valid workflow target and version required", http.StatusBadRequest)
		return
	}
	required := provider.CancelRunConfirmation
	if action == "retry-task" {
		required = provider.RetryTaskConfirmation
	} else if action != "cancel-run" {
		http.Error(response, "unsupported workflow action", http.StatusBadRequest)
		return
	}
	if request.PostForm.Get("confirmation") != required {
		server.renderWorkflows(response, request, sessionID, session, http.StatusBadRequest, "Type the required confirmation exactly.", "")
		return
	}
	scope := "workflow:" + action + ":" + identityBasis(request.PostForm)
	key, err := server.broker.DraftKey(sessionID, scope)
	if err != nil {
		server.renderMetadataError(response, session, err)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	err = server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		if action == "cancel-run" {
			_, callErr := client.CancelWorkflowRun(ctx, id, version, key)
			return callErr
		}
		_, callErr := client.RetryWorkflowTask(ctx, id, version, key)
		return callErr
	})
	if err != nil {
		server.renderOperationError(response, request, sessionID, session, err, server.renderWorkflows)
		return
	}
	server.broker.ClearDraft(sessionID, scope)
	http.Redirect(response, request, "/workflows?updated="+url.QueryEscape(action), http.StatusSeeOther)
}

func (server *authenticatedServer) quotaList(response http.ResponseWriter, request *http.Request) {
	sessionID, session, ok := server.authenticatedSession(response, request)
	if !ok {
		return
	}
	if !session.HasRole("studio.quota.read") {
		http.Error(response, "quota read permission required", http.StatusForbidden)
		return
	}
	server.renderQuota(response, request, sessionID, session, http.StatusOK, "", "")
}

func (server *authenticatedServer) renderQuota(response http.ResponseWriter, request *http.Request, sessionID string, session auth.SessionView, status int, message, requestID string) {
	query := request.URL.Query()
	search, planCursor, usageCursor, discrepancyCursor := query.Get("search"), query.Get("plan_cursor"), query.Get("usage_cursor"), query.Get("discrepancy_cursor")
	if !boundedOperationsQuery(search, planCursor, usageCursor, discrepancyCursor) {
		http.Error(response, "invalid quota query", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	var plans provider.QuotaPlanPage
	var usage provider.UsagePage
	var discrepancies provider.DiscrepancyPage
	err := server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		var listErr error
		plans, listErr = client.ListQuotaPlans(ctx, provider.OperationsQuery{Cursor: planCursor, Limit: 50, Search: search})
		if listErr == nil {
			usage, listErr = client.ListUsage(ctx, provider.OperationsQuery{Cursor: usageCursor, Limit: 50, Search: search})
		}
		if listErr == nil {
			discrepancies, listErr = client.ListQuotaDiscrepancies(ctx, provider.OperationsQuery{Cursor: discrepancyCursor, Limit: 50, Search: search})
		}
		return listErr
	})
	if err != nil {
		server.renderMetadataError(response, session, err)
		return
	}
	data := quotaData{Session: session, Navigation: allowedNavigation(session), Plans: plans.Items, Usage: usage.Items, Discrepancies: discrepancies.Items, Search: search, Message: message, RequestID: requestID, CanAdmin: session.HasRole("studio.quota.admin"), Confirmation: provider.PublishPlanConfirmation}
	if plans.NextCursor != "" {
		data.NextPlans = operationPageURL("/quota", url.Values{"plan_cursor": {plans.NextCursor}, "search": {search}})
	}
	if usage.NextCursor != "" {
		data.NextUsage = operationPageURL("/quota", url.Values{"usage_cursor": {usage.NextCursor}, "search": {search}})
	}
	if discrepancies.NextCursor != "" {
		data.NextDiffs = operationPageURL("/quota", url.Values{"discrepancy_cursor": {discrepancies.NextCursor}, "search": {search}})
	}
	renderTemplate(response, status, quotaTemplate, data)
}

func (server *authenticatedServer) quotaPublish(response http.ResponseWriter, request *http.Request) {
	if !sameSiteMutation(request) {
		http.Error(response, "cross-site quota mutation rejected", http.StatusForbidden)
		return
	}
	sessionID, session, ok := server.authenticatedSession(response, request)
	if !ok {
		return
	}
	if !session.HasRole("studio.quota.admin") {
		http.Error(response, "quota administrator permission required", http.StatusForbidden)
		return
	}
	if !parseOperationForm(response, request) || server.broker.ValidateCSRF(sessionID, request.PostForm.Get("csrf")) != nil {
		http.Error(response, "quota publication rejected", http.StatusForbidden)
		return
	}
	if request.PostForm.Get("confirmation") != provider.PublishPlanConfirmation {
		server.renderQuota(response, request, sessionID, session, http.StatusBadRequest, "Type the required confirmation exactly.", "")
		return
	}
	version, versionErr := parseIdentityVersion(request.PostForm.Get("expected_version"))
	window, windowErr := strconv.ParseInt(request.PostForm.Get("window_seconds"), 10, 64)
	limits, limitsErr := decodeLimits(request.PostForm.Get("limits"))
	plan := provider.QuotaPlan{Code: request.PostForm.Get("code"), Version: version, EffectiveAt: request.PostForm.Get("effective_at"), WindowSeconds: window, Limits: limits}
	if versionErr != nil || windowErr != nil || limitsErr != nil {
		http.Error(response, "valid quota plan fields required", http.StatusBadRequest)
		return
	}
	scope := "quota:publish:" + identityBasis(request.PostForm)
	key, err := server.broker.DraftKey(sessionID, scope)
	if err != nil {
		server.renderMetadataError(response, session, err)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	err = server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		_, callErr := client.PublishQuotaPlan(ctx, plan, version, key)
		return callErr
	})
	if err != nil {
		server.renderOperationError(response, request, sessionID, session, err, server.renderQuota)
		return
	}
	server.broker.ClearDraft(sessionID, scope)
	http.Redirect(response, request, "/quota?updated=published", http.StatusSeeOther)
}

func (server *authenticatedServer) auditList(response http.ResponseWriter, request *http.Request) {
	sessionID, session, ok := server.authenticatedSession(response, request)
	if !ok {
		return
	}
	if !session.HasRole("studio.audit.read") {
		http.Error(response, "audit read permission required", http.StatusForbidden)
		return
	}
	search, cursor := request.URL.Query().Get("search"), request.URL.Query().Get("cursor")
	if !boundedOperationsQuery(search, cursor) {
		http.Error(response, "invalid audit query", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	var page provider.AuditPage
	err := server.withProvider(ctx, sessionID, func(client *provider.Client) error {
		var listErr error
		page, listErr = client.ListAudit(ctx, provider.OperationsQuery{Cursor: cursor, Limit: 50, Search: search})
		return listErr
	})
	if err != nil {
		server.renderMetadataError(response, session, err)
		return
	}
	data := auditData{Session: session, Navigation: allowedNavigation(session), Items: page.Items, Search: search}
	if page.NextCursor != "" {
		data.Next = operationPageURL("/audit", url.Values{"cursor": {page.NextCursor}, "search": {search}})
	}
	renderTemplate(response, http.StatusOK, auditTemplate, data)
}

type operationRenderer func(http.ResponseWriter, *http.Request, string, auth.SessionView, int, string, string)

func (server *authenticatedServer) renderOperationError(response http.ResponseWriter, request *http.Request, sessionID string, session auth.SessionView, err error, render operationRenderer) {
	var api *provider.APIError
	if errors.As(err, &api) {
		message := api.Message
		if message == "" {
			message = "The provider rejected the operation."
		}
		render(response, request, sessionID, session, api.Status, message, api.RequestID)
		return
	}
	server.renderMetadataError(response, session, err)
}

func boundedOperationsQuery(values ...string) bool {
	for index, value := range values {
		limit := 2048
		if index == 0 {
			limit = 256
		}
		if !boundedBrowserValue(value, limit) {
			return false
		}
	}
	return true
}
func operationPageURL(path string, values url.Values) string {
	for key, entries := range values {
		if len(entries) == 1 && entries[0] == "" {
			values.Del(key)
		}
	}
	return path + "?" + values.Encode()
}
func parseOperationForm(response http.ResponseWriter, request *http.Request) bool {
	request.Body = http.MaxBytesReader(response, request.Body, maxMetadataFormBytes)
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid operation form", http.StatusBadRequest)
		return false
	}
	return true
}
func decodeLimits(value string) (map[string]any, error) {
	if value == "" || len(value) > 64<<10 {
		return nil, errors.New("quota limits required")
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	var limits map[string]any
	if err := decoder.Decode(&limits); err != nil || limits == nil {
		return nil, errors.New("quota limits must be a JSON object")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("quota limits contain trailing JSON")
	}
	return limits, nil
}
