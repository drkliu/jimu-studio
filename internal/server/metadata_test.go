package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drkliu/jimu-studio/internal/auth"
	"github.com/drkliu/jimu-studio/internal/provider"
)

type metadataIdentity struct{}

func (metadataIdentity) AuthorizationURL(request auth.AuthorizationRequest) string {
	return "https://identity.example/authorize?state=" + request.State
}

func (metadataIdentity) Exchange(_ context.Context, request auth.ExchangeRequest) (auth.Identity, error) {
	return auth.Identity{
		Subject: "operator", DisplayName: "Metadata operator",
		Roles:       []string{"studio.metadata.read", "studio.metadata.write"},
		AccessToken: request.Code + ".access", RefreshToken: request.Code + ".refresh",
		Expiry: time.Now().Add(time.Hour), Nonce: request.ExpectedNonce,
	}, nil
}

func (metadataIdentity) Refresh(context.Context, string) (auth.Token, error) {
	return auth.Token{}, nil
}

func TestMetadataListIsAccessibleAndCursorBounded(t *testing.T) {
	t.Parallel()
	var receivedLimit string
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		receivedLimit = request.URL.Query().Get("limit")
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"items":[{"code":"orders","name":"Orders","kind":"standard","version":4,"fields":[{"code":"id","data_type":"uuid","required":true,"read_only":true}]}],"next_cursor":"page-2"}`))
	}))
	t.Cleanup(upstream.Close)
	handler, session := metadataHandler(t, upstream)
	request := httptest.NewRequest(http.MethodGet, "/metadata?search=orders", nil)
	request.AddCookie(&http.Cookie{Name: "studio_session", Value: session.ID})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, expected := range []string{"Metadata", "Orders", "id", "uuid", "<table", "page-2"} {
		if !strings.Contains(body, expected) {
			t.Errorf("metadata page omitted %q", expected)
		}
	}
	if receivedLimit != "50" {
		t.Fatalf("provider limit = %q, want 50", receivedLimit)
	}
}

func TestMetadataConflictRefetchesAndKeepsStableIdempotencyKey(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var idempotency []string
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPut {
			var body struct {
				ExpectedVersion int64  `json:"expected_version"`
				IdempotencyKey  string `json:"idempotency_key"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.ExpectedVersion != 4 {
				t.Errorf("expected_version=%d", body.ExpectedVersion)
			}
			mu.Lock()
			idempotency = append(idempotency, body.IdempotencyKey)
			mu.Unlock()
			response.WriteHeader(http.StatusConflict)
			_, _ = response.Write([]byte(`{"code":"version_conflict","message":"entity changed","request_id":"request-9"}`))
			return
		}
		_, _ = response.Write([]byte(`{"items":[{"code":"orders","name":"Orders current","kind":"standard","version":5,"fields":[{"code":"id","data_type":"uuid","required":true,"read_only":true}]}]}`))
	}))
	t.Cleanup(upstream.Close)
	handler, session := metadataHandler(t, upstream)
	form := url.Values{
		"csrf": {session.CSRF}, "code": {"orders"}, "name": {"Orders submitted"}, "kind": {"standard"},
		"expected_version": {"4"}, "field_count": {"1"}, "field_code_0": {"id"}, "field_type_0": {"uuid"}, "field_required_0": {"on"},
	}
	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/metadata/edit", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.AddCookie(&http.Cookie{Name: "studio_session", Value: session.ID})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "Orders submitted") || !strings.Contains(response.Body.String(), "Orders current") || !strings.Contains(response.Body.String(), "request-9") {
			t.Fatalf("attempt %d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(idempotency) != 2 || idempotency[0] == "" || idempotency[0] != idempotency[1] {
		t.Fatalf("idempotency keys = %#v", idempotency)
	}
}

func metadataHandler(t *testing.T, upstream *httptest.Server) (http.Handler, auth.SessionResult) {
	t.Helper()
	broker, err := auth.NewBroker(auth.Config{
		Tenants: []auth.Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: upstream.URL, Identity: metadataIdentity{}}},
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
	session, err := broker.Complete(context.Background(), pending.ID, pending.State, "metadata-code")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(broker, false)
	if err != nil {
		t.Fatal(err)
	}
	return handler, session
}
