// Package localprovider supplies bounded PostgreSQL-backed Studio data for local UI testing.
package localprovider

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	studioprovider "github.com/drkliu/jimu-studio/internal/provider"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const sampleTime = "2026-07-19T00:00:00Z"

var schemaNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// Config identifies the PostgreSQL database and isolated schema used by the provider.
type Config struct {
	DSN    string
	Schema string
}

// NewHandler opens and migrates a PostgreSQL-backed development provider handler.
func NewHandler(ctx context.Context, config Config) (*handler, error) {
	if strings.TrimSpace(config.DSN) == "" {
		return nil, errors.New("PostgreSQL DSN is required")
	}
	if config.Schema == "" {
		config.Schema = "jimu_studio_local"
	}
	if !schemaNamePattern.MatchString(config.Schema) {
		return nil, fmt.Errorf("invalid PostgreSQL schema %q", config.Schema)
	}
	db, err := sql.Open("pgx", config.DSN)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	local := &handler{db: db, schema: config.Schema, stateTable: `"` + config.Schema + `"."state"`}
	if err := local.migrate(ctx, config.Schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return local, nil
}

func initialState() persistentState {
	local := persistentState{
		Entities: map[string]studioprovider.Entity{
			"orders": {
				Code: "orders", Name: "Orders", Kind: "standard", Version: 1,
				Fields: []studioprovider.Field{
					{Code: "id", DataType: "uuid", Required: true, ReadOnly: true},
					{Code: "status", DataType: "string", Required: true},
				},
			},
		},
		Idempotency: make(map[string]cachedMutation),
		Users: map[string]studioprovider.User{
			"local-admin": {ID: "local-admin", DisplayName: "Local Studio Administrator", Email: "admin@example.com", Status: "active", Roles: append([]string(nil), sampleRoleKeys...), Version: 1},
		},
		Roles: make(map[string]studioprovider.Role),
		Run:   studioprovider.WorkflowRun{ID: "run-001", Workflow: "local-order-processing", State: "running", Version: 1, CreatedAt: sampleTime, UpdatedAt: sampleTime, LeaseState: "active", LeaseExpiresAt: sampleTime},
		Task:  studioprovider.WorkflowTask{ID: "task-001", RunID: "run-001", Code: "validate-order", State: "running", Attempt: 1, Version: 1, LeaseState: "active", LeaseExpiresAt: sampleTime, RecoveryState: "none"},
		Plan:  studioprovider.QuotaPlan{Code: "local-default", Version: 1, EffectiveAt: sampleTime, WindowSeconds: 60, Limits: map[string]any{"requests": 1000}},
		Audits: []any{map[string]any{
			"id": "audit-001", "actor_user_id": "local-admin", "action": "studio.login", "target_type": "session",
			"target_id": "local-session", "occurred_at": sampleTime, "details": map[string]any{"environment": "local"}, "redacted_paths": []string{},
		}},
	}
	for _, key := range sampleRoleKeys {
		local.Roles[key] = studioprovider.Role{Key: key, DisplayName: key, System: true, Version: 1}
	}
	return local
}

type handler struct {
	db          *sql.DB
	schema      string
	stateTable  string
	requestMu   sync.Mutex
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

type persistentState struct {
	Entities    map[string]studioprovider.Entity `json:"entities"`
	Idempotency map[string]cachedMutation        `json:"idempotency"`
	Audits      []any                            `json:"audits"`
	Users       map[string]studioprovider.User   `json:"users"`
	Roles       map[string]studioprovider.Role   `json:"roles"`
	Run         studioprovider.WorkflowRun       `json:"run"`
	Task        studioprovider.WorkflowTask      `json:"task"`
	Plan        studioprovider.QuotaPlan         `json:"plan"`
}

type cachedMutation struct {
	Status int `json:"status"`
	Value  any `json:"value"`
}

func (provider *handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	if request.URL.Path == "/healthz" {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := provider.db.PingContext(ctx); err != nil {
			writeJSON(response, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "storage": "postgresql"})
			return
		}
		writeJSON(response, http.StatusOK, map[string]string{"status": "ok", "storage": "postgresql"})
		return
	}
	if !strings.HasPrefix(request.Header.Get("Authorization"), "Bearer ") {
		writeJSON(response, http.StatusUnauthorized, map[string]string{"code": "unauthorized", "message": "Bearer token required"})
		return
	}
	provider.servePersisted(response, request)
}

func (provider *handler) serveLoaded(response http.ResponseWriter, request *http.Request) {

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
			"entity_code": "orders", "plan_code": "local-orders-v1", "risk": "low", "summary": "Local PostgreSQL migration preview", "requires_confirmation": request.URL.Path == "/studio/v1/metadata/apply",
		}})
	default:
		writeJSON(response, http.StatusNotFound, map[string]string{"code": "not_found", "message": "Local provider route not implemented"})
	}
}

func (provider *handler) migrate(ctx context.Context, schema string) error {
	if err := provider.db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	if _, err := provider.db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS "`+schema+`"`); err != nil {
		return fmt.Errorf("create PostgreSQL schema: %w", err)
	}
	if _, err := provider.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+provider.stateTable+` (
		id smallint PRIMARY KEY CHECK (id = 1),
		revision bigint NOT NULL DEFAULT 1,
		payload jsonb NOT NULL,
		updated_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create PostgreSQL state table: %w", err)
	}
	seed, err := json.Marshal(initialState())
	if err != nil {
		return fmt.Errorf("encode initial state: %w", err)
	}
	if _, err := provider.db.ExecContext(ctx, `INSERT INTO `+provider.stateTable+` (id, payload) VALUES (1, $1) ON CONFLICT (id) DO NOTHING`, seed); err != nil {
		return fmt.Errorf("seed PostgreSQL state: %w", err)
	}
	return nil
}

func (provider *handler) servePersisted(response http.ResponseWriter, request *http.Request) {
	provider.requestMu.Lock()
	defer provider.requestMu.Unlock()
	tx, err := provider.db.BeginTx(request.Context(), nil)
	if err != nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"code": "storage_unavailable", "message": "PostgreSQL transaction could not start"})
		return
	}
	defer func() { _ = tx.Rollback() }()
	var payload []byte
	if err := tx.QueryRowContext(request.Context(), `SELECT payload FROM `+provider.stateTable+` WHERE id = 1 FOR UPDATE`).Scan(&payload); err != nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"code": "storage_unavailable", "message": "PostgreSQL state could not be loaded"})
		return
	}
	var state persistentState
	if err := json.Unmarshal(payload, &state); err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"code": "storage_corrupt", "message": "PostgreSQL state is invalid"})
		return
	}
	provider.loadState(state)
	buffer := newBufferedResponse()
	provider.serveLoaded(buffer, request)
	if buffer.status < http.StatusInternalServerError {
		encoded, encodeErr := json.Marshal(provider.snapshotState())
		if encodeErr != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{"code": "storage_encode_failed", "message": "Provider state could not be encoded"})
			return
		}
		if _, err = tx.ExecContext(request.Context(), `UPDATE `+provider.stateTable+` SET payload = $1, revision = revision + 1, updated_at = now() WHERE id = 1`, encoded); err != nil {
			writeJSON(response, http.StatusServiceUnavailable, map[string]string{"code": "storage_unavailable", "message": "PostgreSQL state could not be saved"})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{"code": "storage_unavailable", "message": "PostgreSQL transaction could not commit"})
		return
	}
	buffer.copyTo(response)
}

func (provider *handler) loadState(state persistentState) {
	provider.entities = state.Entities
	provider.idempotency = state.Idempotency
	provider.audits = state.Audits
	provider.users = state.Users
	provider.roles = state.Roles
	provider.run = state.Run
	provider.task = state.Task
	provider.plan = state.Plan
	if provider.entities == nil {
		provider.entities = make(map[string]studioprovider.Entity)
	}
	if provider.idempotency == nil {
		provider.idempotency = make(map[string]cachedMutation)
	}
	if provider.users == nil {
		provider.users = make(map[string]studioprovider.User)
	}
	if provider.roles == nil {
		provider.roles = make(map[string]studioprovider.Role)
	}
}

func (provider *handler) snapshotState() persistentState {
	return persistentState{Entities: provider.entities, Idempotency: provider.idempotency, Audits: provider.audits, Users: provider.users, Roles: provider.roles, Run: provider.run, Task: provider.task, Plan: provider.plan}
}

// Close releases PostgreSQL connections owned by the handler.
func (provider *handler) Close() error { return provider.db.Close() }

// DropTestSchema removes only an explicitly test-scoped schema.
func (provider *handler) DropTestSchema(ctx context.Context) error {
	if !strings.HasPrefix(provider.schema, "jimu_studio_test_") && !strings.HasPrefix(provider.schema, "jimu_studio_e2e_") {
		return fmt.Errorf("refuse to drop non-test schema %q", provider.schema)
	}
	_, err := provider.db.ExecContext(ctx, `DROP SCHEMA "`+provider.schema+`" CASCADE`)
	return err
}

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: make(http.Header), status: http.StatusOK}
}
func (response *bufferedResponse) Header() http.Header            { return response.header }
func (response *bufferedResponse) WriteHeader(status int)         { response.status = status }
func (response *bufferedResponse) Write(body []byte) (int, error) { return response.body.Write(body) }
func (response *bufferedResponse) copyTo(target http.ResponseWriter) {
	for key, values := range response.header {
		for _, value := range values {
			target.Header().Add(key, value)
		}
	}
	target.Header().Set("Content-Type", "application/json; charset=utf-8")
	target.Header().Set("X-Content-Type-Options", "nosniff")
	target.WriteHeader(response.status)
	_, _ = target.Write(response.body.Bytes())
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
		writeJSON(response, cached.Status, cached.Value)
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
	provider.idempotency[body.IdempotencyKey] = cachedMutation{Status: http.StatusOK, Value: updated}
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
		writeJSON(response, cached.Status, cached.Value)
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
	provider.idempotency[body.IdempotencyKey] = cachedMutation{Status: http.StatusOK, Value: plan}
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
		writeJSON(response, cached.Status, cached.Value)
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
	provider.idempotency[body.IdempotencyKey] = cachedMutation{Status: http.StatusOK, Value: receipt}
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
