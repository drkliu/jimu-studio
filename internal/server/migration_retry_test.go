package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/drkliu/jimu-studio/internal/provider"
)

func TestMigrationApplyRetryKeepsStableIdempotencyKey(t *testing.T) {
	t.Parallel()
	var keys []string
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/studio/v1/metadata/entities":
			_, _ = response.Write([]byte(`{"items":[{"code":"orders","name":"Orders","version":9,"fields":[]}]}`))
		case "/studio/v1/metadata/plan":
			_, _ = response.Write([]byte(`[{"entity_code":"orders","plan_code":"orders_plan","risk":"low","summary":"Prepare Orders","requires_confirmation":true}]`))
		case "/studio/v1/metadata/apply":
			var body struct {
				IdempotencyKey string `json:"idempotency_key"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			keys = append(keys, body.IdempotencyKey)
			response.WriteHeader(http.StatusInternalServerError)
			_, _ = response.Write([]byte(`{"code":"temporary","message":"internal provider detail","request_id":"retry-apply"}`))
		}
	}))
	t.Cleanup(upstream.Close)
	handler, session := metadataHandler(t, upstream)
	draft := plannedMigrationReference(t, handler, session.ID, session.CSRF)
	for attempt := 0; attempt < 2; attempt++ {
		response := submitMigrationForm(t, handler, session.ID, "/metadata/apply", url.Values{
			"csrf": {session.CSRF}, "draft": {draft}, "confirmation": {provider.ApplyMigrationConfirmation},
		})
		if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "internal provider detail") || !strings.Contains(response.Body.String(), "retry-apply") {
			t.Fatalf("attempt=%d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	if len(keys) != 2 || keys[0] == "" || keys[0] != keys[1] {
		t.Fatalf("idempotency keys=%v", keys)
	}
}

func TestMigrationConflictInvalidatesPlanAndRequiresReplan(t *testing.T) {
	t.Parallel()
	applyCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/studio/v1/metadata/entities":
			_, _ = response.Write([]byte(`{"items":[{"code":"orders","name":"Orders","version":9,"fields":[]}]}`))
		case "/studio/v1/metadata/plan":
			_, _ = response.Write([]byte(`[{"entity_code":"orders","plan_code":"orders_plan","risk":"high","summary":"Prepare Orders","requires_confirmation":true}]`))
		case "/studio/v1/metadata/apply":
			applyCalls++
			response.WriteHeader(http.StatusConflict)
			_, _ = response.Write([]byte(`{"code":"version_conflict","message":"stale entity","request_id":"stale-apply"}`))
		}
	}))
	t.Cleanup(upstream.Close)
	handler, session := metadataHandler(t, upstream)
	draft := plannedMigrationReference(t, handler, session.ID, session.CSRF)
	values := url.Values{"csrf": {session.CSRF}, "draft": {draft}, "confirmation": {provider.ApplyMigrationConfirmation}}
	first := submitMigrationForm(t, handler, session.ID, "/metadata/apply", values)
	if first.Code != http.StatusConflict || !strings.Contains(first.Body.String(), "create and review a fresh plan") || !strings.Contains(first.Body.String(), "stale-apply") {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	second := submitMigrationForm(t, handler, session.ID, "/metadata/apply", values)
	if second.Code != http.StatusConflict || applyCalls != 1 {
		t.Fatalf("second status=%d apply_calls=%d", second.Code, applyCalls)
	}
}

func plannedMigrationReference(t *testing.T, handler http.Handler, sessionID, csrf string) string {
	t.Helper()
	response := submitMigrationForm(t, handler, sessionID, "/metadata/plan", url.Values{"csrf": {csrf}, "code": {"orders"}})
	if response.Code != http.StatusOK {
		t.Fatalf("plan status=%d body=%s", response.Code, response.Body.String())
	}
	match := regexp.MustCompile(`name="draft" value="([^"]+)"`).FindStringSubmatch(response.Body.String())
	if len(match) != 2 {
		t.Fatalf("draft reference missing: %s", response.Body.String())
	}
	return match[1]
}
