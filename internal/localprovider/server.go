// Package localprovider supplies bounded, in-memory Studio data for local UI testing.
package localprovider

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	studioprovider "github.com/drkliu/jimu-studio/internal/provider"
)

const sampleTime = "2026-07-19T00:00:00Z"

// NewHandler returns a loopback-only development provider handler.
func NewHandler() http.Handler {
	local := &handler{
		entities: map[string]studioprovider.Entity{
			"orders": {
				Code: "orders", Name: "Orders", Kind: "standard", Version: 1,
				Fields: []studioprovider.Field{
					{Code: "id", DataType: "uuid", Required: true, ReadOnly: true},
					{Code: "status", DataType: "string", Required: true},
				},
			},
		},
		idempotency: make(map[string]cachedMutation),
		users: map[string]studioprovider.User{
			"local-admin": {ID: "local-admin", DisplayName: "Local Studio Administrator", Email: "admin@example.com", Status: "active", Roles: append([]string(nil), sampleRoleKeys...), Version: 1},
		},
		roles: make(map[string]studioprovider.Role),
		run:   studioprovider.WorkflowRun{ID: "run-001", Workflow: "local-order-processing", State: "running", Version: 1, CreatedAt: sampleTime, UpdatedAt: sampleTime, LeaseState: "active", LeaseExpiresAt: sampleTime},
		task:  studioprovider.WorkflowTask{ID: "task-001", RunID: "run-001", Code: "validate-order", State: "running", Attempt: 1, Version: 1, LeaseState: "active", LeaseExpiresAt: sampleTime, RecoveryState: "none"},
		plan:  studioprovider.QuotaPlan{Code: "local-default", Version: 1, EffectiveAt: sampleTime, WindowSeconds: 60, Limits: map[string]any{"requests": 1000}},
		audits: []any{map[string]any{
			"id": "audit-001", "actor_user_id": "local-admin", "action": "studio.login", "target_type": "session",
			"target_id": "local-session", "occurred_at": sampleTime, "details": map[string]any{"environment": "local"}, "redacted_paths": []string{},
		}},
	}
	for _, key := range sampleRoleKeys {
		local.roles[key] = studioprovider.Role{Key: key, DisplayName: key, System: true, Version: 1}
	}
	return local
}

type handler struct {
	mu          sync.Mutex
	entities    map[string]studioprovider.Entity
	idempotency map[string]cachedMutation
	audits      []any
	users       map[string]studioprovider.User
	roles       map[string]studioprovider.Role
	run         studioprovider.WorkflowRun
	task        studioprovider.WorkflowTask
	plan        studioprovider.QuotaPlan
}

type cachedMutation struct {
	status int
	value  any
}

func (provider *handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	if request.URL.Path == "/healthz" {
		writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if !strings.HasPrefix(request.Header.Get("Authorization"), "Bearer ") {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"code": "unauthorized", "message": "Bearer token required"})
		return
	}

	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/studio/v1/metadata/entities":
		provider.listEntities(response)
	case request.Method == http.MethodPut && strings.HasPrefix(request.URL.Path, "/studio/v1/metadata/entities/"):
		provider.updateEntity(response, request)
	case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/delete-plan"):
		provider.planEntityDeletion(response, request)
	case request.Method == http.MethodDelete && strings.HasPrefix(request.URL.Path, "/studio/v1/metadata/entities/"):
		provider.deleteEntity(response, request)
	case request.Method == http.MethodGet && request.URL.Path == "/studio/v1/identity/users":
		provider.listUsers(response)
	case request.Method == http.MethodPost && request.URL.Path == "/studio/v1/identity/users":
		provider.createUser(response, request)
	case request.Method == http.MethodPatch && strings.HasSuffix(request.URL.Path, "/status"):
		provider.setUserStatus(response, request)
	case request.Method == http.MethodGet && request.URL.Path == "/studio/v1/identity/roles":
		provider.listRoles(response)
	case request.Method == http.MethodPost && request.URL.Path == "/studio/v1/identity/roles":
		provider.createRole(response, request)
	case request.Method == http.MethodPatch && strings.HasPrefix(request.URL.Path, "/studio/v1/identity/roles/"):
		provider.updateRole(response, request)
	case (request.Method == http.MethodPut || request.Method == http.MethodDelete) && strings.Contains(request.URL.Path, "/roles/"):
		provider.changeUserRole(response, request)
	case request.Method == http.MethodGet && request.URL.Path == "/studio/v1/workflows/runs":
		provider.mu.Lock()
		run := provider.run
		provider.mu.Unlock()
		writeJSON(response, http.StatusOK, map[string]any{"items": []any{run}})
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/studio/v1/workflows/runs/") && strings.HasSuffix(request.URL.Path, "/tasks"):
		provider.mu.Lock()
		task := provider.task
		provider.mu.Unlock()
		writeJSON(response, http.StatusOK, map[string]any{"items": []any{task}})
	case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/cancel"):
		provider.cancelRun(response, request)
	case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/retry"):
		provider.retryTask(response, request)
	case request.Method == http.MethodGet && request.URL.Path == "/studio/v1/quota/plans":
		provider.mu.Lock()
		plan := provider.plan
		provider.mu.Unlock()
		writeJSON(response, http.StatusOK, map[string]any{"items": []any{plan}})
	case request.Method == http.MethodPost && request.URL.Path == "/studio/v1/quota/plans":
		provider.publishPlan(response, request)
	case request.Method == http.MethodGet && request.URL.Path == "/studio/v1/quota/usage":
		writeJSON(response, http.StatusOK, map[string]any{"items": []any{map[string]any{
			"route": "/orders", "unit": "request", "quantity": 42, "occurred_at": sampleTime, "plan_code": "local-default", "plan_version": 1,
		}}})
	case request.Method == http.MethodGet && request.URL.Path == "/studio/v1/quota/discrepancies":
		writeJSON(response, http.StatusOK, map[string]any{"items": []any{map[string]any{
			"id": "difference-001", "route": "/orders", "unit": "request", "from": sampleTime, "until": sampleTime,
			"enforcement_total": 42, "ledger_total": 42, "plan_code": "local-default", "plan_version": 1,
		}}})
	case request.Method == http.MethodGet && request.URL.Path == "/studio/v1/audit":
		provider.mu.Lock()
		audits := append([]any(nil), provider.audits...)
		provider.mu.Unlock()
		writeJSON(response, http.StatusOK, map[string]any{"items": audits})
	case request.Method == http.MethodPost && (request.URL.Path == "/studio/v1/metadata/plan" || request.URL.Path == "/studio/v1/metadata/apply"):
		writeJSON(response, http.StatusOK, []any{map[string]any{
			"entity_code": "orders", "plan_code": "local-orders-v1", "risk": "low", "summary": "Local in-memory migration preview", "requires_confirmation": request.URL.Path == "/studio/v1/metadata/apply",
		}})
	default:
		writeJSON(response, http.StatusNotFound, map[string]string{"code": "not_found", "message": "Local provider route not implemented"})
	}
}

func (provider *handler) listEntities(response http.ResponseWriter) {
	provider.mu.Lock()
	codes := make([]string, 0, len(provider.entities))
	for code := range provider.entities {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	entities := make([]studioprovider.Entity, 0, len(codes))
	for _, code := range codes {
		entities = append(entities, cloneEntity(provider.entities[code]))
	}
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, map[string]any{"items": entities})
}

func (provider *handler) updateEntity(response http.ResponseWriter, request *http.Request) {
	var body struct {
		Entity          studioprovider.Entity `json:"entity"`
		ExpectedVersion int64                 `json:"expected_version"`
		IdempotencyKey  string                `json:"idempotency_key"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 64<<10))
	if err := decoder.Decode(&body); err != nil || body.Entity.Code == "" || body.Entity.Name == "" || body.IdempotencyKey == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_entity", "message": "Valid entity required"})
		return
	}
	code := strings.TrimPrefix(request.URL.Path, "/studio/v1/metadata/entities/")
	if code != body.Entity.Code || strings.Contains(code, "/") {
		writeJSON(response, http.StatusBadRequest, map[string]string{"code": "entity_code_mismatch", "message": "Entity path and body code must match"})
		return
	}
	provider.mu.Lock()
	if cached, ok := provider.idempotency[body.IdempotencyKey]; ok {
		provider.mu.Unlock()
		writeJSON(response, cached.status, cached.value)
		return
	}
	current, exists := provider.entities[code]
	if (exists && current.Version != body.ExpectedVersion) || (!exists && body.ExpectedVersion != 0) {
		provider.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"code": "version_conflict", "message": "Entity version changed"})
		return
	}
	if exists {
		body.Entity.Version = current.Version + 1
	} else {
		body.Entity.Version = 1
	}
	provider.entities[code] = cloneEntity(body.Entity)
	updated := cloneEntity(body.Entity)
	provider.idempotency[body.IdempotencyKey] = cachedMutation{status: http.StatusOK, value: updated}
	action := "studio.metadata.entities.create"
	if exists {
		action = "studio.metadata.entities.update"
	}
	provider.appendAuditLocked(action, code, map[string]any{"version": updated.Version})
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, updated)
}

func (provider *handler) planEntityDeletion(response http.ResponseWriter, request *http.Request) {
	code := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/studio/v1/metadata/entities/"), "/delete-plan")
	body, ok := decodeDeletionMutation(response, request)
	if !ok {
		return
	}
	provider.mu.Lock()
	if cached, found := provider.idempotency[body.IdempotencyKey]; found {
		provider.mu.Unlock()
		writeJSON(response, cached.status, cached.value)
		return
	}
	entity, found := provider.entities[code]
	if !found {
		provider.mu.Unlock()
		writeJSON(response, http.StatusNotFound, map[string]string{"code": "entity_not_found", "message": "Entity was not found"})
		return
	}
	if entity.Version != body.ExpectedVersion {
		provider.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"code": "version_conflict", "message": "Entity version changed"})
		return
	}
	plan := studioprovider.DeletionPlan{
		Code: code, ExpectedVersion: entity.Version, Deletable: true, Dependencies: []string{},
		ImpactSummary: "The local provider found no dependent metadata definitions.",
	}
	provider.idempotency[body.IdempotencyKey] = cachedMutation{status: http.StatusOK, value: plan}
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, plan)
}

func (provider *handler) deleteEntity(response http.ResponseWriter, request *http.Request) {
	code := strings.TrimPrefix(request.URL.Path, "/studio/v1/metadata/entities/")
	body, ok := decodeDeletionMutation(response, request)
	if !ok {
		return
	}
	if body.Confirmation != studioprovider.DeleteEntityConfirmation {
		writeJSON(response, http.StatusBadRequest, map[string]string{"code": "confirmation_required", "message": "Exact deletion confirmation required"})
		return
	}
	provider.mu.Lock()
	if cached, found := provider.idempotency[body.IdempotencyKey]; found {
		provider.mu.Unlock()
		writeJSON(response, cached.status, cached.value)
		return
	}
	entity, found := provider.entities[code]
	if !found {
		provider.mu.Unlock()
		writeJSON(response, http.StatusNotFound, map[string]string{"code": "entity_not_found", "message": "Entity was not found"})
		return
	}
	if entity.Version != body.ExpectedVersion {
		provider.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"code": "version_conflict", "message": "Entity version changed"})
		return
	}
	delete(provider.entities, code)
	receipt := studioprovider.DeletionReceipt{Code: code, DeletedVersion: entity.Version, DeletedAt: time.Now().UTC().Format(time.RFC3339)}
	provider.idempotency[body.IdempotencyKey] = cachedMutation{status: http.StatusOK, value: receipt}
	provider.appendAuditLocked("studio.metadata.entities.delete", code, map[string]any{"deleted_version": entity.Version})
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, receipt)
}

type deletionMutation struct {
	Confirmation    string `json:"confirmation"`
	ExpectedVersion int64  `json:"expected_version"`
	IdempotencyKey  string `json:"idempotency_key"`
}

func decodeDeletionMutation(response http.ResponseWriter, request *http.Request) (deletionMutation, bool) {
	var body deletionMutation
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 64<<10))
	if err := decoder.Decode(&body); err != nil || body.ExpectedVersion < 0 || body.IdempotencyKey == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_mutation", "message": "Expected version and idempotency key required"})
		return deletionMutation{}, false
	}
	return body, true
}

func (provider *handler) appendAuditLocked(action, targetID string, details map[string]any) {
	provider.audits = append(provider.audits, map[string]any{
		"id": "audit-local-" + time.Now().UTC().Format("20060102150405.000000000"), "actor_user_id": "local-admin",
		"action": action, "target_type": "metadata_entity", "target_id": targetID,
		"occurred_at": time.Now().UTC().Format(time.RFC3339), "details": details, "redacted_paths": []string{},
	})
}

func cloneEntity(source studioprovider.Entity) studioprovider.Entity {
	source.Fields = append([]studioprovider.Field(nil), source.Fields...)
	return source
}

func sampleUser() map[string]any {
	roles := make([]string, 0, len(sampleRoleKeys))
	roles = append(roles, sampleRoleKeys...)
	return map[string]any{"id": "local-admin", "display_name": "Local Studio Administrator", "email": "admin@example.com", "status": "active", "roles": roles, "version": 1}
}

var sampleRoleKeys = []string{
	"identity.admin", "studio.audit.read", "studio.metadata.apply", "studio.metadata.read", "studio.metadata.write",
	"studio.quota.admin", "studio.quota.read", "studio.workflow.operate", "studio.workflow.read",
}

func sampleRoles() []any {
	roles := make([]any, 0, len(sampleRoleKeys))
	for _, key := range sampleRoleKeys {
		roles = append(roles, map[string]any{"key": key, "display_name": key, "system": true, "version": 1})
	}
	return roles
}

func sampleRun() map[string]any {
	return map[string]any{
		"id": "run-001", "workflow": "local-order-processing", "state": "running", "version": 1,
		"created_at": sampleTime, "updated_at": sampleTime, "lease_state": "active", "lease_expires_at": sampleTime,
	}
}

func sampleTask(runID string) map[string]any {
	return map[string]any{
		"id": "task-001", "run_id": runID, "code": "validate-order", "state": "running", "attempt": 1, "version": 1,
		"lease_state": "active", "lease_expires_at": sampleTime, "recovery_state": "none",
	}
}

func samplePlan() map[string]any {
	return map[string]any{
		"code": "local-default", "version": 1, "effective_at": sampleTime, "window_seconds": 60,
		"limits": map[string]any{"requests": 1000},
	}
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
