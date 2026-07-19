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

func TestMetadataCreateUsesVersionZeroAndAccessibleEditor(t *testing.T) {
	t.Parallel()
	var created struct {
		Entity          provider.Entity `json:"entity"`
		ExpectedVersion int64           `json:"expected_version"`
		IdempotencyKey  string          `json:"idempotency_key"`
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.Method != http.MethodPut || request.URL.EscapedPath() != "/studio/v1/metadata/entities/customers" {
			http.Error(response, "unexpected request", http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(request.Body).Decode(&created); err != nil {
			t.Fatal(err)
		}
		_, _ = response.Write([]byte(`{"code":"customers","name":"Customers","kind":"standard","version":1,"fields":[{"code":"id","data_type":"uuid","required":true}]}`))
	}))
	t.Cleanup(upstream.Close)
	handler, session := metadataHandler(t, upstream)

	get := httptest.NewRequest(http.MethodGet, "/metadata/new", nil)
	get.AddCookie(&http.Cookie{Name: "studio_session", Value: session.ID})
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, get)
	for _, expected := range []string{"Create entity type", "data-add-field", "data-remove-field", "/assets/metadata-fields.js"} {
		if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), expected) {
			t.Fatalf("create editor status=%d omitted %q: %s", getResponse.Code, expected, getResponse.Body.String())
		}
	}

	form := url.Values{
		"csrf": {session.CSRF}, "code": {"customers"}, "name": {"Customers"}, "kind": {"standard"},
		"expected_version": {"0"}, "field_count": {"1"}, "field_code_0": {"id"}, "field_type_0": {"uuid"}, "field_required_0": {"on"},
	}
	response := submitMigrationForm(t, handler, session.ID, "/metadata/create", form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	if created.ExpectedVersion != 0 || created.IdempotencyKey == "" || created.Entity.Code != "customers" || len(created.Entity.Fields) != 1 {
		t.Fatalf("create body=%#v", created)
	}
}

func TestMetadataDeleteRequiresReviewedDraftAndExactConfirmation(t *testing.T) {
	t.Parallel()
	var deleteCalls int
	var deleted struct {
		Confirmation    string `json:"confirmation"`
		ExpectedVersion int64  `json:"expected_version"`
		IdempotencyKey  string `json:"idempotency_key"`
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/studio/v1/metadata/entities":
			_, _ = response.Write([]byte(`{"items":[{"code":"orders","name":"Orders","version":7,"fields":[]}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/studio/v1/metadata/entities/orders/delete-plan":
			_, _ = response.Write([]byte(`{"code":"orders","expected_version":7,"deletable":true,"dependencies":[],"impact_summary":"No dependencies."}`))
		case request.Method == http.MethodDelete && request.URL.Path == "/studio/v1/metadata/entities/orders":
			deleteCalls++
			if err := json.NewDecoder(request.Body).Decode(&deleted); err != nil {
				t.Fatal(err)
			}
			_, _ = response.Write([]byte(`{"code":"orders","deleted_version":7,"deleted_at":"2026-07-19T01:02:03Z"}`))
		default:
			http.Error(response, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)
	handler, session := metadataHandler(t, upstream)

	plan := submitMigrationForm(t, handler, session.ID, "/metadata/delete/plan", url.Values{"csrf": {session.CSRF}, "code": {"orders"}})
	if plan.Code != http.StatusOK || !strings.Contains(plan.Body.String(), "No dependencies.") {
		t.Fatalf("plan status=%d body=%s", plan.Code, plan.Body.String())
	}
	match := regexp.MustCompile(`name="draft" value="([^"]+)"`).FindStringSubmatch(plan.Body.String())
	if len(match) != 2 || match[1] == "" || strings.Contains(match[1], "orders") {
		t.Fatalf("opaque deletion draft missing: %v", match)
	}
	draft := match[1]

	wrong := submitMigrationForm(t, handler, session.ID, "/metadata/delete/apply", url.Values{
		"csrf": {session.CSRF}, "draft": {draft}, "confirmation": {"delete_entity"},
	})
	if wrong.Code != http.StatusBadRequest || deleteCalls != 0 {
		t.Fatalf("wrong confirmation status=%d delete_calls=%d", wrong.Code, deleteCalls)
	}

	applied := submitMigrationForm(t, handler, session.ID, "/metadata/delete/apply", url.Values{
		"csrf": {session.CSRF}, "draft": {draft}, "confirmation": {provider.DeleteEntityConfirmation},
	})
	if applied.Code != http.StatusOK || !strings.Contains(applied.Body.String(), "Entity deleted") {
		t.Fatalf("delete status=%d body=%s", applied.Code, applied.Body.String())
	}
	if deleteCalls != 1 || deleted.Confirmation != provider.DeleteEntityConfirmation || deleted.ExpectedVersion != 7 || deleted.IdempotencyKey == "" {
		t.Fatalf("delete calls=%d body=%#v", deleteCalls, deleted)
	}

	replay := submitMigrationForm(t, handler, session.ID, "/metadata/delete/apply", url.Values{
		"csrf": {session.CSRF}, "draft": {draft}, "confirmation": {provider.DeleteEntityConfirmation},
	})
	if replay.Code != http.StatusConflict || deleteCalls != 1 {
		t.Fatalf("replay status=%d delete_calls=%d", replay.Code, deleteCalls)
	}
}

func TestMetadataDeleteRequiresApplyRoleBeforeProvider(t *testing.T) {
	t.Parallel()
	providerCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		providerCalls++
		http.Error(response, "unexpected provider call", http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	handler, session := metadataWriterHandler(t, upstream)
	response := submitMigrationForm(t, handler, session.ID, "/metadata/delete/plan", url.Values{"csrf": {session.CSRF}, "code": {"orders"}})
	if response.Code != http.StatusForbidden || providerCalls != 0 {
		t.Fatalf("status=%d provider_calls=%d body=%s", response.Code, providerCalls, response.Body.String())
	}
}

func TestMetadataDeleteDependencyBlockCreatesNoApplicableDraft(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/studio/v1/metadata/entities" {
			_, _ = response.Write([]byte(`{"items":[{"code":"orders","name":"Orders","version":7,"fields":[]}]}`))
			return
		}
		_, _ = response.Write([]byte(`{"code":"orders","expected_version":7,"deletable":false,"dependencies":["workflow:checkout"],"impact_summary":"A workflow still depends on this entity."}`))
	}))
	t.Cleanup(upstream.Close)
	handler, session := metadataHandler(t, upstream)
	response := submitMigrationForm(t, handler, session.ID, "/metadata/delete/plan", url.Values{"csrf": {session.CSRF}, "code": {"orders"}})
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "workflow:checkout") || strings.Contains(body, `name="draft"`) || strings.Contains(body, "Permanently delete entity") {
		t.Fatalf("status=%d body=%s", response.Code, body)
	}
}
