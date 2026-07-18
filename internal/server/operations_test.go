package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/drkliu/jimu-studio/internal/auth"
	"github.com/drkliu/jimu-studio/internal/provider"
)

type operationsIdentity struct{}

func (operationsIdentity) AuthorizationURL(request auth.AuthorizationRequest) string {
	return "https://identity.example/authorize?state=" + request.State
}

func (operationsIdentity) Exchange(_ context.Context, request auth.ExchangeRequest) (auth.Identity, error) {
	return auth.Identity{
		Subject: "operator", DisplayName: "Operations operator",
		Roles:       []string{"studio.workflow.read", "studio.workflow.operate", "studio.quota.read", "studio.quota.admin", "studio.audit.read"},
		AccessToken: request.Code + ".access", RefreshToken: request.Code + ".refresh",
		Expiry: time.Now().Add(time.Hour), Nonce: request.ExpectedNonce,
	}, nil
}

func (operationsIdentity) Refresh(context.Context, string) (auth.Token, error) {
	return auth.Token{}, nil
}

func TestOperationsPagesAreBoundedAccessibleAndRedacted(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Query().Get("limit") != "50" {
			t.Errorf("limit for %s = %q", request.URL.Path, request.URL.Query().Get("limit"))
		}
		switch request.URL.Path {
		case "/studio/v1/workflows/runs":
			_, _ = response.Write([]byte(`{"items":[{"id":"run-1","workflow":"checkout","state":"running","version":7,"created_at":"2026-07-16T10:00:00Z","updated_at":"2026-07-16T10:01:00Z","lease_state":"active"}]}`))
		case "/studio/v1/workflows/runs/run-1/tasks":
			_, _ = response.Write([]byte(`{"items":[{"id":"task-1","run_id":"run-1","code":"charge","state":"failed","attempt":2,"version":9,"error_code":"provider_timeout","lease_state":"expired","recovery_state":"lease_expired"}]}`))
		case "/studio/v1/quota/plans", "/studio/v1/quota/usage", "/studio/v1/quota/discrepancies":
			_, _ = response.Write([]byte(`{"items":[]}`))
		case "/studio/v1/audit":
			_, _ = response.Write([]byte(`{"items":[{"id":"audit-1","actor_user_id":"operator","action":"workflow.retry","target_type":"task","target_id":"task-1","occurred_at":"2026-07-16T10:02:00Z","details":{"secret":"must-not-render"},"redacted_paths":["details.secret"]}]}`))
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(upstream.Close)
	handler, session := operationsHandler(t, upstream)
	for _, test := range []struct {
		path     string
		expected []string
		rejected string
	}{
		{path: "/workflows", expected: []string{"Workflow operations", "checkout", "charge", "provider_timeout", "lease_expired"}},
		{path: "/quota", expected: []string{"Quota, usage, and reconciliation", "observational", provider.PublishPlanConfirmation}},
		{path: "/audit", expected: []string{"Redacted audit trail", "workflow.retry", "details.secret"}, rejected: "must-not-render"},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.AddCookie(&http.Cookie{Name: "studio_session", Value: session.ID})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", test.path, response.Code, response.Body.String())
		}
		for _, expected := range test.expected {
			if !strings.Contains(response.Body.String(), expected) {
				t.Errorf("GET %s omitted %q", test.path, expected)
			}
		}
		if test.rejected != "" && strings.Contains(response.Body.String(), test.rejected) {
			t.Errorf("GET %s rendered sensitive audit detail", test.path)
		}
	}
}

func TestWorkflowRetryUsesDisplayedTaskVersionAndExactConfirmation(t *testing.T) {
	t.Parallel()
	var received struct {
		Confirmation    string `json:"confirmation"`
		ExpectedVersion int64  `json:"expected_version"`
		IdempotencyKey  string `json:"idempotency_key"`
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/studio/v1/workflows/tasks/task-1/retry" {
			if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
				t.Fatal(err)
			}
			_, _ = response.Write([]byte(`{"id":"run-1","workflow":"checkout","state":"running","version":8,"created_at":"2026-07-16T10:00:00Z","updated_at":"2026-07-16T10:04:00Z","lease_state":"none"}`))
			return
		}
		http.NotFound(response, request)
	}))
	t.Cleanup(upstream.Close)
	handler, session := operationsHandler(t, upstream)
	form := url.Values{"csrf": {session.CSRF}, "action": {"retry-task"}, "id": {"task-1"}, "expected_version": {"9"}, "confirmation": {provider.RetryTaskConfirmation}}
	request := httptest.NewRequest(http.MethodPost, "/workflows/mutate", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.AddCookie(&http.Cookie{Name: "studio_session", Value: session.ID})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || received.Confirmation != provider.RetryTaskConfirmation || received.ExpectedVersion != 9 || received.IdempotencyKey == "" {
		t.Fatalf("status=%d mutation=%#v body=%s", response.Code, received, response.Body.String())
	}
}

func operationsHandler(t *testing.T, upstream *httptest.Server) (http.Handler, auth.SessionResult) {
	t.Helper()
	broker, err := auth.NewBroker(auth.Config{
		Tenants: []auth.Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: upstream.URL, Identity: operationsIdentity{}}},
		ClientFactory: func(ctx context.Context, baseURL, token string) (auth.TenantClient, error) {
			return provider.NewClient(ctx, baseURL, token, upstream.Client().Transport)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := broker.Begin("alpha")
	if err != nil {
		t.Fatal(err)
	}
	session, err := broker.Complete(context.Background(), pending.ID, pending.State, "operations-code")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(broker, false)
	if err != nil {
		t.Fatal(err)
	}
	return handler, session
}
