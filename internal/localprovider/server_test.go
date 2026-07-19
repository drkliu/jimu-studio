package localprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

var testSchemaSequence atomic.Uint64

func TestUnauthorizedResponseHasJSONSecurityHeaders(t *testing.T) {
	response := httptest.NewRecorder()
	(&handler{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/studio/v1/metadata/entities", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", response.Code)
	}
	if response.Header().Get("Content-Type") != "application/json; charset=utf-8" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unsafe response headers: %v", response.Header())
	}
}

func newTestHandler(t *testing.T) *handler {
	t.Helper()
	dsn := os.Getenv("JIMU_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("JIMU_TEST_PG_DSN is required for PostgreSQL integration tests")
	}
	schema := fmt.Sprintf("jimu_studio_test_%d_%d", time.Now().UnixNano(), testSchemaSequence.Add(1))
	handler, err := NewHandler(context.Background(), Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatalf("open PostgreSQL test handler: %v", err)
	}
	t.Cleanup(func() {
		_, _ = handler.db.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`)
		_ = handler.Close()
	})
	return handler
}

func TestMetadataRequiresBearerAndReturnsBoundedSample(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/studio/v1/metadata/entities?limit=50", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/studio/v1/metadata/entities?limit=50", nil)
	request.Header.Set("Authorization", "Bearer local-test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if response.Code != http.StatusOK || json.NewDecoder(response.Body).Decode(&page) != nil || len(page.Items) != 1 || page.Items[0]["code"] != "orders" {
		t.Fatalf("metadata response = %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/json; charset=utf-8" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unsafe response headers: %v", response.Header())
	}
}

func TestMetadataCreateDeleteAndAuditAreRecorded(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if body == "" {
			reader = bytes.NewReader(nil)
		} else {
			reader = bytes.NewReader([]byte(body))
		}
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Authorization", "Bearer local-test-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}

	created := request(http.MethodPut, "/studio/v1/metadata/entities/customers", `{"entity":{"code":"customers","name":"Customers","version":0,"fields":[]},"expected_version":0,"idempotency_key":"create-customers"}`)
	if created.Code != http.StatusOK || !bytes.Contains(created.Body.Bytes(), []byte(`"version":1`)) {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	plan := request(http.MethodPost, "/studio/v1/metadata/entities/customers/delete-plan", `{"expected_version":1,"idempotency_key":"plan-delete-customers"}`)
	if plan.Code != http.StatusOK || !bytes.Contains(plan.Body.Bytes(), []byte(`"deletable":true`)) {
		t.Fatalf("plan=%d %s", plan.Code, plan.Body.String())
	}
	deleted := request(http.MethodDelete, "/studio/v1/metadata/entities/customers", `{"confirmation":"DELETE_ENTITY","expected_version":1,"idempotency_key":"delete-customers"}`)
	if deleted.Code != http.StatusOK || !bytes.Contains(deleted.Body.Bytes(), []byte(`"deleted_version":1`)) {
		t.Fatalf("delete=%d %s", deleted.Code, deleted.Body.String())
	}
	list := request(http.MethodGet, "/studio/v1/metadata/entities?limit=50", "")
	if list.Code != http.StatusOK || bytes.Contains(list.Body.Bytes(), []byte(`customers`)) {
		t.Fatalf("list after delete=%d %s", list.Code, list.Body.String())
	}
	audit := request(http.MethodGet, "/studio/v1/audit?limit=50", "")
	if audit.Code != http.StatusOK || !bytes.Contains(audit.Body.Bytes(), []byte(`studio.metadata.entities.create`)) || !bytes.Contains(audit.Body.Bytes(), []byte(`studio.metadata.entities.delete`)) {
		t.Fatalf("audit=%d %s", audit.Code, audit.Body.String())
	}
}

func TestAllLocalRoleMutationsAreExecutableAndAudited(t *testing.T) {
	t.Parallel()
	handler := newTestHandler(t)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Bearer local-test-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	mutations := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/studio/v1/identity/users", `{"id":"operator","display_name":"Operator","email":"operator@example.com","idempotency_key":"create-user"}`},
		{http.MethodPost, "/studio/v1/identity/roles", `{"key":"custom.operator","display_name":"Custom operator","idempotency_key":"create-role"}`},
		{http.MethodPatch, "/studio/v1/identity/roles/custom.operator", `{"display_name":"Updated operator","expected_version":1,"idempotency_key":"update-role"}`},
		{http.MethodPut, "/studio/v1/identity/users/operator/roles/custom.operator", `{"expected_version":1,"idempotency_key":"assign-role"}`},
		{http.MethodDelete, "/studio/v1/identity/users/operator/roles/custom.operator", `{"expected_version":2,"idempotency_key":"revoke-role"}`},
		{http.MethodPatch, "/studio/v1/identity/users/operator/status", `{"confirmation":"DISABLE_USER","status":"disabled","expected_version":3,"idempotency_key":"disable-user"}`},
		{http.MethodPost, "/studio/v1/workflows/runs/run-001/cancel", `{"confirmation":"CANCEL_RUN","expected_version":1,"idempotency_key":"cancel-run"}`},
		{http.MethodPost, "/studio/v1/workflows/tasks/task-001/retry", `{"confirmation":"RETRY_TASK","expected_version":1,"idempotency_key":"retry-task"}`},
		{http.MethodPost, "/studio/v1/quota/plans", `{"confirmation":"PUBLISH_QUOTA_PLAN","expected_version":1,"idempotency_key":"publish-plan","plan":{"code":"local-default","version":1,"effective_at":"2026-07-19T00:00:00Z","window_seconds":60,"limits":{"requests":2000}}}`},
	}
	for _, mutation := range mutations {
		response := request(mutation.method, mutation.path, mutation.body)
		if response.Code != http.StatusOK {
			t.Fatalf("%s %s = %d %s", mutation.method, mutation.path, response.Code, response.Body.String())
		}
	}
	audit := request(http.MethodGet, "/studio/v1/audit?limit=50", "")
	for _, action := range []string{
		"studio.identity.users.create", "studio.identity.roles.create", "studio.identity.roles.update",
		"studio.identity.roles.assign", "studio.identity.roles.revoke", "studio.identity.users.status",
		"studio.workflow.runs.cancel", "studio.workflow.tasks.retry", "studio.quota.plans.publish",
	} {
		if audit.Code != http.StatusOK || !bytes.Contains(audit.Body.Bytes(), []byte(action)) {
			t.Fatalf("audit omitted %s: %d %s", action, audit.Code, audit.Body.String())
		}
	}
}

func TestMetadataPersistsAcrossProviderRestart(t *testing.T) {
	t.Parallel()
	dsn := os.Getenv("JIMU_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("JIMU_TEST_PG_DSN is required for PostgreSQL integration tests")
	}
	schema := fmt.Sprintf("jimu_studio_restart_%d_%d", time.Now().UnixNano(), testSchemaSequence.Add(1))
	open := func() *handler {
		result, err := NewHandler(context.Background(), Config{DSN: dsn, Schema: schema})
		if err != nil {
			t.Fatalf("open PostgreSQL test handler: %v", err)
		}
		return result
	}
	request := func(target http.Handler, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Bearer local-test-token")
		response := httptest.NewRecorder()
		target.ServeHTTP(response, req)
		return response
	}

	first := open()
	created := request(first, http.MethodPut, "/studio/v1/metadata/entities/persistent", `{"entity":{"code":"persistent","name":"Persistent","version":0,"fields":[]},"expected_version":0,"idempotency_key":"create-persistent"}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create before restart = %d %s", created.Code, created.Body.String())
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first provider: %v", err)
	}

	second := open()
	t.Cleanup(func() {
		_, _ = second.db.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`)
		_ = second.Close()
	})
	listed := request(second, http.MethodGet, "/studio/v1/metadata/entities?limit=50", "")
	if listed.Code != http.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte(`"code":"persistent"`)) {
		t.Fatalf("list after restart = %d %s", listed.Code, listed.Body.String())
	}
}
