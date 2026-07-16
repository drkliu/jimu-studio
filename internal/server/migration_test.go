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

func TestMigrationApplyRequiresReviewedServerDraftAndExactConfirmation(t *testing.T) {
	t.Parallel()
	var applyCalls int
	var applied struct {
		Confirmation   string            `json:"confirmation"`
		Entities       []provider.Entity `json:"entities"`
		IdempotencyKey string            `json:"idempotency_key"`
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/studio/v1/metadata/entities":
			_, _ = response.Write([]byte(`{"items":[{"code":"orders","name":"Orders","version":7,"fields":[{"code":"id","data_type":"uuid"}]}]}`))
		case "/studio/v1/metadata/plan":
			_, _ = response.Write([]byte(`[{"entity_code":"orders","plan_code":"add_orders","risk":"medium","summary":"Create storage for Orders","requires_confirmation":true}]`))
		case "/studio/v1/metadata/apply":
			applyCalls++
			if err := json.NewDecoder(request.Body).Decode(&applied); err != nil {
				t.Fatal(err)
			}
			_, _ = response.Write([]byte(`[{"entity_code":"orders","plan_code":"add_orders","risk":"medium","summary":"Created storage for Orders","requires_confirmation":true}]`))
		default:
			http.Error(response, "unexpected path", http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)
	handler, session := metadataHandler(t, upstream)

	planResponse := submitMigrationForm(t, handler, session.ID, "/metadata/plan", url.Values{"csrf": {session.CSRF}, "code": {"orders"}})
	if planResponse.Code != http.StatusOK || !strings.Contains(planResponse.Body.String(), "Create storage for Orders") || !strings.Contains(planResponse.Body.String(), "medium") {
		t.Fatalf("plan status=%d body=%s", planResponse.Code, planResponse.Body.String())
	}
	match := regexp.MustCompile(`name="draft" value="([^"]+)"`).FindStringSubmatch(planResponse.Body.String())
	if len(match) != 2 || match[1] == "" || strings.Contains(match[1], "orders") {
		t.Fatalf("opaque draft reference missing: %v", match)
	}
	draft := match[1]

	wrong := submitMigrationForm(t, handler, session.ID, "/metadata/apply", url.Values{
		"csrf": {session.CSRF}, "draft": {draft}, "confirmation": {"apply_migration"},
	})
	if wrong.Code != http.StatusBadRequest || applyCalls != 0 {
		t.Fatalf("wrong confirmation status=%d apply_calls=%d", wrong.Code, applyCalls)
	}

	appliedResponse := submitMigrationForm(t, handler, session.ID, "/metadata/apply", url.Values{
		"csrf": {session.CSRF}, "draft": {draft}, "confirmation": {provider.ApplyMigrationConfirmation},
	})
	if appliedResponse.Code != http.StatusOK || !strings.Contains(appliedResponse.Body.String(), "Migration applied") {
		t.Fatalf("apply status=%d body=%s", appliedResponse.Code, appliedResponse.Body.String())
	}
	if applyCalls != 1 || applied.Confirmation != provider.ApplyMigrationConfirmation || len(applied.Entities) != 1 || applied.Entities[0].Version != 7 || applied.IdempotencyKey == "" {
		t.Fatalf("apply_calls=%d body=%#v", applyCalls, applied)
	}

	replay := submitMigrationForm(t, handler, session.ID, "/metadata/apply", url.Values{
		"csrf": {session.CSRF}, "draft": {draft}, "confirmation": {provider.ApplyMigrationConfirmation},
	})
	if replay.Code != http.StatusConflict || applyCalls != 1 {
		t.Fatalf("replay status=%d apply_calls=%d", replay.Code, applyCalls)
	}
}

func TestMigrationPlanProviderDenialIsAuthoritative(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/studio/v1/metadata/entities" {
			_, _ = response.Write([]byte(`{"items":[{"code":"orders","name":"Orders","version":7,"fields":[]}]}`))
			return
		}
		response.WriteHeader(http.StatusForbidden)
		_, _ = response.Write([]byte(`{"code":"forbidden","message":"provider denied migration plan","request_id":"deny-plan"}`))
	}))
	t.Cleanup(upstream.Close)
	handler, session := metadataHandler(t, upstream)
	response := submitMigrationForm(t, handler, session.ID, "/metadata/plan", url.Values{"csrf": {session.CSRF}, "code": {"orders"}})
	if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "provider denied migration plan") || !strings.Contains(response.Body.String(), "provider denied this migration operation") || !strings.Contains(response.Body.String(), "deny-plan") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func submitMigrationForm(t *testing.T, handler http.Handler, sessionID, path string, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.AddCookie(&http.Cookie{Name: "studio_session", Value: sessionID})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
