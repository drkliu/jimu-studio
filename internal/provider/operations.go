package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	CancelRunConfirmation   = "CANCEL_RUN"
	RetryTaskConfirmation   = "RETRY_TASK"
	PublishPlanConfirmation = "PUBLISH_QUOTA_PLAN"
)

type OperationsQuery struct {
	Cursor string
	Limit  int
	Search string
}

type WorkflowRun struct {
	ID             string `json:"id"`
	Workflow       string `json:"workflow"`
	State          string `json:"state"`
	Version        int64  `json:"version"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	LeaseState     string `json:"lease_state"`
	LeaseExpiresAt string `json:"lease_expires_at,omitempty"`
	NextWakeAt     string `json:"next_wake_at,omitempty"`
}

type WorkflowTask struct {
	ID             string `json:"id"`
	RunID          string `json:"run_id"`
	Code           string `json:"code"`
	State          string `json:"state"`
	Attempt        int64  `json:"attempt"`
	Version        int64  `json:"version"`
	NextAttemptAt  string `json:"next_attempt_at,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	LeaseState     string `json:"lease_state"`
	LeaseExpiresAt string `json:"lease_expires_at,omitempty"`
	RecoveryState  string `json:"recovery_state"`
}

type WorkflowRunPage struct {
	Items      []WorkflowRun `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}
type WorkflowTaskPage struct {
	Items      []WorkflowTask `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type QuotaPlan struct {
	Code          string         `json:"code"`
	Version       int64          `json:"version"`
	EffectiveAt   string         `json:"effective_at"`
	WindowSeconds int64          `json:"window_seconds"`
	Limits        map[string]any `json:"limits"`
}
type UsageRecord struct {
	Route       string `json:"route"`
	Unit        string `json:"unit"`
	Quantity    int64  `json:"quantity"`
	OccurredAt  string `json:"occurred_at"`
	PlanCode    string `json:"plan_code"`
	PlanVersion int64  `json:"plan_version"`
}
type QuotaDiscrepancy struct {
	ID               string `json:"id"`
	Route            string `json:"route"`
	Unit             string `json:"unit"`
	From             string `json:"from"`
	Until            string `json:"until"`
	EnforcementTotal int64  `json:"enforcement_total"`
	LedgerTotal      int64  `json:"ledger_total"`
	PlanCode         string `json:"plan_code"`
	PlanVersion      int64  `json:"plan_version"`
}
type AuditRecord struct {
	ID            string         `json:"id"`
	ActorUserID   string         `json:"actor_user_id"`
	Action        string         `json:"action"`
	TargetType    string         `json:"target_type"`
	TargetID      string         `json:"target_id"`
	OccurredAt    string         `json:"occurred_at"`
	Details       map[string]any `json:"details"`
	RedactedPaths []string       `json:"redacted_paths"`
}

type QuotaPlanPage struct {
	Items      []QuotaPlan `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
}
type UsagePage struct {
	Items      []UsageRecord `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}
type DiscrepancyPage struct {
	Items      []QuotaDiscrepancy `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}
type AuditPage struct {
	Items      []AuditRecord `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func (client *Client) ListWorkflowRuns(ctx context.Context, query OperationsQuery) (WorkflowRunPage, error) {
	var page WorkflowRunPage
	err := client.listOperation(ctx, "/studio/v1/workflows/runs", query, &page)
	if err == nil {
		err = validatePage(query.Limit, page.NextCursor, len(page.Items))
		for _, item := range page.Items {
			if err == nil {
				err = validateRun(item)
			}
		}
	}
	return page, err
}

func (client *Client) ListWorkflowTasks(ctx context.Context, runID string, query OperationsQuery) (WorkflowTaskPage, error) {
	if !safePathValue(runID, 256) {
		return WorkflowTaskPage{}, errors.New("safe workflow run ID required")
	}
	var page WorkflowTaskPage
	err := client.listOperation(ctx, "/studio/v1/workflows/runs/"+url.PathEscape(runID)+"/tasks", query, &page)
	if err == nil {
		err = validatePage(query.Limit, page.NextCursor, len(page.Items))
		for _, item := range page.Items {
			if err == nil {
				err = validateTask(item, runID)
			}
		}
	}
	return page, err
}

func (client *Client) CancelWorkflowRun(ctx context.Context, id string, version int64, key string) (WorkflowRun, error) {
	return client.workflowMutation(ctx, "/studio/v1/workflows/runs/"+url.PathEscape(id)+"/cancel", id, version, key, CancelRunConfirmation)
}

func (client *Client) RetryWorkflowTask(ctx context.Context, id string, version int64, key string) (WorkflowRun, error) {
	return client.workflowMutation(ctx, "/studio/v1/workflows/tasks/"+url.PathEscape(id)+"/retry", id, version, key, RetryTaskConfirmation)
}

func (client *Client) workflowMutation(ctx context.Context, path, id string, version int64, key, confirmation string) (WorkflowRun, error) {
	if !safePathValue(id, 256) || version < 0 || !safeIdentityKey(key) {
		return WorkflowRun{}, errors.New("safe workflow mutation required")
	}
	body := struct {
		Confirmation    string `json:"confirmation"`
		ExpectedVersion int64  `json:"expected_version"`
		IdempotencyKey  string `json:"idempotency_key"`
	}{confirmation, version, key}
	var result WorkflowRun
	err := client.operationRequest(ctx, http.MethodPost, path, body, &result)
	if err == nil {
		err = validateRun(result)
	}
	return result, err
}

func (client *Client) ListQuotaPlans(ctx context.Context, query OperationsQuery) (QuotaPlanPage, error) {
	var p QuotaPlanPage
	e := client.listOperation(ctx, "/studio/v1/quota/plans", query, &p)
	if e == nil {
		e = validatePage(query.Limit, p.NextCursor, len(p.Items))
		for _, v := range p.Items {
			if e == nil {
				e = validatePlan(v)
			}
		}
	}
	return p, e
}
func (client *Client) ListUsage(ctx context.Context, query OperationsQuery) (UsagePage, error) {
	var p UsagePage
	e := client.listOperation(ctx, "/studio/v1/quota/usage", query, &p)
	if e == nil {
		e = validatePage(query.Limit, p.NextCursor, len(p.Items))
		for _, v := range p.Items {
			if e == nil {
				e = validateUsage(v)
			}
		}
	}
	return p, e
}
func (client *Client) ListQuotaDiscrepancies(ctx context.Context, query OperationsQuery) (DiscrepancyPage, error) {
	var p DiscrepancyPage
	e := client.listOperation(ctx, "/studio/v1/quota/discrepancies", query, &p)
	if e == nil {
		e = validatePage(query.Limit, p.NextCursor, len(p.Items))
		for _, v := range p.Items {
			if e == nil {
				e = validateDiscrepancy(v)
			}
		}
	}
	return p, e
}
func (client *Client) ListAudit(ctx context.Context, query OperationsQuery) (AuditPage, error) {
	var p AuditPage
	e := client.listOperation(ctx, "/studio/v1/audit", query, &p)
	if e == nil {
		e = validatePage(query.Limit, p.NextCursor, len(p.Items))
		for _, v := range p.Items {
			if e == nil {
				e = validateAudit(v)
			}
		}
	}
	return p, e
}

func (client *Client) PublishQuotaPlan(ctx context.Context, plan QuotaPlan, expectedVersion int64, key string) (QuotaPlan, error) {
	if err := validatePlan(plan); err != nil {
		return QuotaPlan{}, err
	}
	if expectedVersion < 0 || !safeIdentityKey(key) {
		return QuotaPlan{}, errors.New("safe quota publication required")
	}
	body := struct {
		Confirmation    string    `json:"confirmation"`
		ExpectedVersion int64     `json:"expected_version"`
		IdempotencyKey  string    `json:"idempotency_key"`
		Plan            QuotaPlan `json:"plan"`
	}{PublishPlanConfirmation, expectedVersion, key, plan}
	var result QuotaPlan
	err := client.operationRequest(ctx, http.MethodPost, "/studio/v1/quota/plans", body, &result)
	if err == nil {
		err = validatePlan(result)
	}
	return result, err
}

func (client *Client) listOperation(ctx context.Context, path string, query OperationsQuery, target any) error {
	if query.Limit < 1 || query.Limit > 200 || len(query.Cursor) > 2048 || len(query.Search) > 256 || hasControl(query.Cursor) || hasControl(query.Search) {
		return errors.New("operations query exceeds bounds")
	}
	values := url.Values{"limit": {strconv.Itoa(query.Limit)}}
	if query.Cursor != "" {
		values.Set("cursor", query.Cursor)
	}
	if query.Search != "" {
		values.Set("search", query.Search)
	}
	return client.operationRequest(ctx, http.MethodGet, path+"?"+values.Encode(), nil, target)
}
func (client *Client) operationRequest(ctx context.Context, method, path string, body, target any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	response, err := client.Do(ctx, method, path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	return decodeProviderResponse(response, target)
}
func validatePage(limit int, cursor string, count int) error {
	if count > limit || len(cursor) > 2048 || hasControl(cursor) {
		return errors.New("provider returned unbounded page")
	}
	return nil
}
func validTime(value string, optional bool) bool {
	if value == "" {
		return optional
	}
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}
func boundedText(value string, limit int) bool {
	return value != "" && len(value) <= limit && !hasControl(value)
}
func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
func validateRun(v WorkflowRun) error {
	if !safePathValue(v.ID, 256) || !boundedText(v.Workflow, 256) || !boundedText(v.State, 64) || v.Version < 0 || !validTime(v.CreatedAt, false) || !validTime(v.UpdatedAt, false) || !oneOf(v.LeaseState, "none", "active", "expired") || !validTime(v.LeaseExpiresAt, true) || !validTime(v.NextWakeAt, true) {
		return errors.New("workflow run exceeds bounds")
	}
	return nil
}
func validateTask(v WorkflowTask, runID string) error {
	if !safePathValue(v.ID, 256) || v.RunID != runID || !safePathValue(v.RunID, 256) || !boundedText(v.Code, 256) || v.Attempt < 0 || v.Version < 0 || !oneOf(v.State, "pending", "running", "succeeded", "retry_waiting", "failed", "compensating", "compensated", "compensation_failed") || !oneOf(v.LeaseState, "none", "active", "expired") || !oneOf(v.RecoveryState, "none", "retry_waiting", "lease_expired") || len(v.ErrorCode) > 256 || hasControl(v.ErrorCode) || !validTime(v.NextAttemptAt, true) || !validTime(v.LeaseExpiresAt, true) {
		return errors.New("workflow task exceeds bounds")
	}
	return nil
}
func validatePlan(v QuotaPlan) error {
	if !safePathValue(v.Code, 256) || v.Version < 0 || v.WindowSeconds < 1 || !validTime(v.EffectiveAt, false) || len(v.Limits) > 200 {
		return errors.New("quota plan exceeds bounds")
	}
	data, err := json.Marshal(v.Limits)
	if err != nil || len(data) > 64<<10 {
		return errors.New("quota limits exceed bounds")
	}
	return nil
}
func validateUsage(v UsageRecord) error {
	if !boundedText(v.Route, 512) || !boundedText(v.Unit, 128) || v.Quantity < 0 || !validTime(v.OccurredAt, false) || !safePathValue(v.PlanCode, 256) || v.PlanVersion < 0 {
		return errors.New("usage record exceeds bounds")
	}
	return nil
}
func validateDiscrepancy(v QuotaDiscrepancy) error {
	if !safePathValue(v.ID, 256) || !boundedText(v.Route, 512) || !boundedText(v.Unit, 128) || !validTime(v.From, false) || !validTime(v.Until, false) || v.EnforcementTotal < 0 || v.LedgerTotal < 0 || !safePathValue(v.PlanCode, 256) || v.PlanVersion < 0 {
		return errors.New("quota discrepancy exceeds bounds")
	}
	return nil
}
func validateAudit(v AuditRecord) error {
	if !safePathValue(v.ID, 256) || !boundedText(v.ActorUserID, 256) || !boundedText(v.Action, 256) || !boundedText(v.TargetType, 256) || !boundedText(v.TargetID, 256) || !validTime(v.OccurredAt, false) || len(v.RedactedPaths) > 200 {
		return errors.New("audit record exceeds bounds")
	}
	data, err := json.Marshal(v.Details)
	if err != nil || len(data) > 64<<10 {
		return errors.New("audit details exceed bounds")
	}
	for _, p := range v.RedactedPaths {
		if !boundedText(p, 512) {
			return fmt.Errorf("audit redaction path exceeds bounds")
		}
	}
	return nil
}
