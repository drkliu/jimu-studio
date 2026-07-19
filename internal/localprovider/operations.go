package localprovider

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	studioprovider "github.com/drkliu/jimu-studio/internal/provider"
)

type versionedMutation struct {
	Confirmation    string `json:"confirmation"`
	ExpectedVersion int64  `json:"expected_version"`
	IdempotencyKey  string `json:"idempotency_key"`
}

func (provider *handler) listUsers(response http.ResponseWriter) {
	provider.mu.Lock()
	keys := make([]string, 0, len(provider.users))
	for key := range provider.users {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	users := make([]studioprovider.User, 0, len(keys))
	for _, key := range keys {
		user := provider.users[key]
		user.Roles = append([]string(nil), user.Roles...)
		users = append(users, user)
	}
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, map[string]any{"items": users})
}

func (provider *handler) listRoles(response http.ResponseWriter) {
	provider.mu.Lock()
	keys := make([]string, 0, len(provider.roles))
	for key := range provider.roles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	roles := make([]studioprovider.Role, 0, len(keys))
	for _, key := range keys {
		roles = append(roles, provider.roles[key])
	}
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, map[string]any{"items": roles})
}

func (provider *handler) createUser(response http.ResponseWriter, request *http.Request) {
	var body struct {
		DisplayName    string `json:"display_name"`
		Email          string `json:"email"`
		ID             string `json:"id"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if !decodeLocalMutation(response, request, &body) || body.ID == "" || body.DisplayName == "" || body.IdempotencyKey == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_user", "message": "Valid user required"})
		return
	}
	provider.mu.Lock()
	if provider.writeCachedLocked(response, body.IdempotencyKey) {
		return
	}
	if _, exists := provider.users[body.ID]; exists {
		provider.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"code": "user_exists", "message": "User already exists"})
		return
	}
	user := studioprovider.User{ID: body.ID, DisplayName: body.DisplayName, Email: body.Email, Status: "active", Roles: []string{}, Version: 1}
	provider.users[user.ID] = user
	provider.cacheAndAuditLocked(body.IdempotencyKey, user, "studio.identity.users.create", user.ID, map[string]any{"version": user.Version})
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, user)
}

func (provider *handler) setUserStatus(response http.ResponseWriter, request *http.Request) {
	var body struct {
		versionedMutation
		Status string `json:"status"`
	}
	if !decodeLocalMutation(response, request, &body) || body.IdempotencyKey == "" || body.Confirmation != studioprovider.DisableUserConfirmation || (body.Status != "active" && body.Status != "disabled") {
		writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_status", "message": "Valid confirmed status required"})
		return
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) != 6 {
		writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_path", "message": "Valid user path required"})
		return
	}
	id := parts[4]
	provider.mu.Lock()
	if provider.writeCachedLocked(response, body.IdempotencyKey) {
		return
	}
	user, exists := provider.users[id]
	if !exists || user.Version != body.ExpectedVersion {
		provider.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"code": "version_conflict", "message": "User version changed"})
		return
	}
	user.Status = body.Status
	user.Version++
	provider.users[id] = user
	provider.cacheAndAuditLocked(body.IdempotencyKey, user, "studio.identity.users.status", id, map[string]any{"status": body.Status, "version": user.Version})
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, user)
}

func (provider *handler) createRole(response http.ResponseWriter, request *http.Request) {
	var body struct {
		DisplayName    string `json:"display_name"`
		IdempotencyKey string `json:"idempotency_key"`
		Key            string `json:"key"`
	}
	if !decodeLocalMutation(response, request, &body) || body.Key == "" || body.DisplayName == "" || body.IdempotencyKey == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_role", "message": "Valid role required"})
		return
	}
	provider.mu.Lock()
	if provider.writeCachedLocked(response, body.IdempotencyKey) {
		return
	}
	if _, exists := provider.roles[body.Key]; exists {
		provider.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"code": "role_exists", "message": "Role already exists"})
		return
	}
	role := studioprovider.Role{Key: body.Key, DisplayName: body.DisplayName, Version: 1}
	provider.roles[role.Key] = role
	provider.cacheAndAuditLocked(body.IdempotencyKey, role, "studio.identity.roles.create", role.Key, map[string]any{"version": role.Version})
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, role)
}

func (provider *handler) updateRole(response http.ResponseWriter, request *http.Request) {
	var body struct {
		versionedMutation
		DisplayName string `json:"display_name"`
	}
	if !decodeLocalMutation(response, request, &body) || body.DisplayName == "" || body.IdempotencyKey == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_role", "message": "Valid role update required"})
		return
	}
	key := strings.TrimPrefix(request.URL.Path, "/studio/v1/identity/roles/")
	provider.mu.Lock()
	if provider.writeCachedLocked(response, body.IdempotencyKey) {
		return
	}
	role, exists := provider.roles[key]
	if !exists || role.Version != body.ExpectedVersion {
		provider.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"code": "version_conflict", "message": "Role version changed"})
		return
	}
	if role.System {
		provider.mu.Unlock()
		writeJSON(response, http.StatusBadRequest, map[string]string{"code": "system_role", "message": "System roles are immutable"})
		return
	}
	role.DisplayName = body.DisplayName
	role.Version++
	provider.roles[key] = role
	provider.cacheAndAuditLocked(body.IdempotencyKey, role, "studio.identity.roles.update", key, map[string]any{"version": role.Version})
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, role)
}

func (provider *handler) changeUserRole(response http.ResponseWriter, request *http.Request) {
	var body versionedMutation
	if !decodeLocalMutation(response, request, &body) || body.IdempotencyKey == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_assignment", "message": "Valid role assignment required"})
		return
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) != 7 {
		writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_path", "message": "Valid role assignment path required"})
		return
	}
	userID, roleKey := parts[4], parts[6]
	provider.mu.Lock()
	if provider.writeCachedLocked(response, body.IdempotencyKey) {
		return
	}
	user, exists := provider.users[userID]
	_, roleExists := provider.roles[roleKey]
	if !exists || !roleExists || user.Version != body.ExpectedVersion {
		provider.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"code": "version_conflict", "message": "User or role changed"})
		return
	}
	roles := make(map[string]bool, len(user.Roles)+1)
	for _, role := range user.Roles {
		roles[role] = true
	}
	if request.Method == http.MethodPut {
		roles[roleKey] = true
	} else {
		delete(roles, roleKey)
	}
	user.Roles = user.Roles[:0]
	for role := range roles {
		user.Roles = append(user.Roles, role)
	}
	sort.Strings(user.Roles)
	user.Version++
	provider.users[userID] = user
	action := "studio.identity.roles.assign"
	if request.Method == http.MethodDelete {
		action = "studio.identity.roles.revoke"
	}
	provider.cacheAndAuditLocked(body.IdempotencyKey, user, action, userID, map[string]any{"role": roleKey, "version": user.Version})
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, user)
}

func (provider *handler) cancelRun(response http.ResponseWriter, request *http.Request) {
	provider.workflowMutation(response, request, studioprovider.CancelRunConfirmation, false)
}

func (provider *handler) retryTask(response http.ResponseWriter, request *http.Request) {
	provider.workflowMutation(response, request, studioprovider.RetryTaskConfirmation, true)
}

func (provider *handler) workflowMutation(response http.ResponseWriter, request *http.Request, confirmation string, retry bool) {
	var body versionedMutation
	if !decodeLocalMutation(response, request, &body) || body.Confirmation != confirmation || body.IdempotencyKey == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_workflow_mutation", "message": "Valid confirmed workflow mutation required"})
		return
	}
	provider.mu.Lock()
	if provider.writeCachedLocked(response, body.IdempotencyKey) {
		return
	}
	if retry {
		if provider.task.Version != body.ExpectedVersion {
			provider.mu.Unlock()
			writeJSON(response, http.StatusConflict, map[string]string{"code": "version_conflict", "message": "Task version changed"})
			return
		}
		provider.task.State = "running"
		provider.task.Attempt++
		provider.task.Version++
		provider.run.State = "running"
		provider.run.Version++
		provider.run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		provider.cacheAndAuditLocked(body.IdempotencyKey, provider.run, "studio.workflow.tasks.retry", provider.task.ID, map[string]any{"version": provider.task.Version})
	} else {
		if provider.run.Version != body.ExpectedVersion {
			provider.mu.Unlock()
			writeJSON(response, http.StatusConflict, map[string]string{"code": "version_conflict", "message": "Run version changed"})
			return
		}
		provider.run.State = "cancelled"
		provider.run.Version++
		provider.run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		provider.run.LeaseState = "none"
		provider.run.LeaseExpiresAt = ""
		provider.cacheAndAuditLocked(body.IdempotencyKey, provider.run, "studio.workflow.runs.cancel", provider.run.ID, map[string]any{"version": provider.run.Version})
	}
	result := provider.run
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, result)
}

func (provider *handler) publishPlan(response http.ResponseWriter, request *http.Request) {
	var body struct {
		versionedMutation
		Plan studioprovider.QuotaPlan `json:"plan"`
	}
	if !decodeLocalMutation(response, request, &body) || body.Confirmation != studioprovider.PublishPlanConfirmation || body.IdempotencyKey == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{"code": "invalid_quota_plan", "message": "Valid confirmed quota plan required"})
		return
	}
	provider.mu.Lock()
	if provider.writeCachedLocked(response, body.IdempotencyKey) {
		return
	}
	if provider.plan.Version != body.ExpectedVersion {
		provider.mu.Unlock()
		writeJSON(response, http.StatusConflict, map[string]string{"code": "version_conflict", "message": "Quota plan version changed"})
		return
	}
	body.Plan.Version = provider.plan.Version + 1
	provider.plan = body.Plan
	provider.cacheAndAuditLocked(body.IdempotencyKey, body.Plan, "studio.quota.plans.publish", body.Plan.Code, map[string]any{"version": body.Plan.Version})
	provider.mu.Unlock()
	writeJSON(response, http.StatusOK, body.Plan)
}

func decodeLocalMutation(response http.ResponseWriter, request *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 64<<10))
	return decoder.Decode(target) == nil
}

// writeCachedLocked writes and unlocks when an idempotent result exists.
func (provider *handler) writeCachedLocked(response http.ResponseWriter, key string) bool {
	cached, ok := provider.idempotency[key]
	if !ok {
		return false
	}
	provider.mu.Unlock()
	writeJSON(response, cached.Status, cached.Value)
	return true
}

func (provider *handler) cacheAndAuditLocked(key string, value any, action, target string, details map[string]any) {
	provider.idempotency[key] = cachedMutation{Status: http.StatusOK, Value: value}
	provider.appendAuditLocked(action, target, details)
}
