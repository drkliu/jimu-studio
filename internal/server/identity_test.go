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

type identityAdminProvider struct{}

func (identityAdminProvider) AuthorizationURL(request auth.AuthorizationRequest) string {
	return "https://identity.example/?state=" + request.State
}
func (identityAdminProvider) Exchange(_ context.Context, request auth.ExchangeRequest) (auth.Identity, error) {
	return auth.Identity{Subject: "admin", DisplayName: "Admin", Roles: []string{"identity.admin"}, AccessToken: request.Code + ".access", RefreshToken: request.Code + ".refresh", Expiry: time.Now().Add(time.Hour), Nonce: request.ExpectedNonce}, nil
}
func (identityAdminProvider) Refresh(context.Context, string) (auth.Token, error) {
	return auth.Token{}, nil
}

func TestIdentityPageIsBoundedCredentialFreeAndProtectsSystemRole(t *testing.T) {
	t.Parallel()
	var limits []string
	mutations := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodGet {
			limits = append(limits, request.URL.Query().Get("limit"))
			if strings.HasSuffix(request.URL.Path, "/users") {
				_, _ = response.Write([]byte(`{"items":[{"id":"admin-1","display_name":"Admin One","email":"admin@example.test","status":"active","roles":["admin"],"version":3}],"next_cursor":"users-next"}`))
			} else {
				_, _ = response.Write([]byte(`{"items":[{"key":"admin","display_name":"Administrator","system":true,"version":2},{"key":"operator","display_name":"Operator","system":false,"version":4}],"next_cursor":"roles-next"}`))
			}
			return
		}
		mutations++
		http.Error(response, "unexpected mutation", http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	handler, session := identityHandler(t, upstream)
	request := httptest.NewRequest(http.MethodGet, "/identity?user_search=admin&role_search=op", nil)
	request.AddCookie(&http.Cookie{Name: "studio_session", Value: session.ID})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "Admin One") || !strings.Contains(body, "System · immutable") || !strings.Contains(body, "users-next") || !strings.Contains(body, "roles-next") {
		t.Fatalf("status=%d body=%s", response.Code, body)
	}
	if strings.Contains(body, `type="password"`) || strings.Contains(body, `name="token"`) || len(limits) != 2 || limits[0] != "50" || limits[1] != "50" {
		t.Fatalf("unsafe page limits=%v", limits)
	}
	form := url.Values{"csrf": {session.CSRF}, "action": {"role-update"}, "role_key": {"admin"}, "display_name": {"Changed"}, "expected_version": {"2"}}
	result := submitIdentity(t, handler, session.ID, form)
	if result.Code != http.StatusConflict || !strings.Contains(result.Body.String(), "System roles are immutable") || mutations != 0 {
		t.Fatalf("status=%d mutations=%d body=%s", result.Code, mutations, result.Body.String())
	}
}

func TestIdentityStatusRequiresExactConfirmationAndKeepsStableConflictKey(t *testing.T) {
	t.Parallel()
	var keys []string
	patches := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodGet {
			if strings.HasSuffix(request.URL.Path, "/users") {
				_, _ = response.Write([]byte(`{"items":[{"id":"admin-1","display_name":"Admin One","status":"active","roles":["admin"],"version":3}]}`))
			} else {
				_, _ = response.Write([]byte(`{"items":[{"key":"admin","display_name":"Administrator","system":true,"version":2}]}`))
			}
			return
		}
		patches++
		var body struct {
			Confirmation    string `json:"confirmation"`
			ExpectedVersion int64  `json:"expected_version"`
			IdempotencyKey  string `json:"idempotency_key"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Confirmation != provider.DisableUserConfirmation || body.ExpectedVersion != 3 {
			t.Errorf("body=%#v", body)
		}
		keys = append(keys, body.IdempotencyKey)
		response.WriteHeader(http.StatusConflict)
		_, _ = response.Write([]byte(`{"code":"last_admin","message":"cannot disable the last active administrator","request_id":"last-admin"}`))
	}))
	t.Cleanup(upstream.Close)
	handler, session := identityHandler(t, upstream)
	base := url.Values{"csrf": {session.CSRF}, "action": {"user-status"}, "user_id": {"admin-1"}, "status": {"disabled"}, "expected_version": {"3"}}
	wrong := cloneValues(base)
	wrong.Set("confirmation", "disable_user")
	response := submitIdentity(t, handler, session.ID, wrong)
	if response.Code != http.StatusBadRequest || patches != 0 {
		t.Fatalf("wrong status=%d patches=%d", response.Code, patches)
	}
	for attempt := 0; attempt < 2; attempt++ {
		values := cloneValues(base)
		values.Set("confirmation", provider.DisableUserConfirmation)
		response = submitIdentity(t, handler, session.ID, values)
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "cannot disable the last active administrator") || !strings.Contains(response.Body.String(), "last-admin") {
			t.Fatalf("attempt=%d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	if len(keys) != 2 || keys[0] == "" || keys[0] != keys[1] {
		t.Fatalf("keys=%v", keys)
	}
}

func identityHandler(t *testing.T, upstream *httptest.Server) (http.Handler, auth.SessionResult) {
	t.Helper()
	broker, err := auth.NewBroker(auth.Config{Tenants: []auth.Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: upstream.URL, Identity: identityAdminProvider{}}}, ClientFactory: func(ctx context.Context, baseURL, token string) (auth.TenantClient, error) {
		return provider.NewClient(ctx, baseURL, token, upstream.Client().Transport)
	}})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := broker.Begin("alpha")
	if err != nil {
		t.Fatal(err)
	}
	session, err := broker.Complete(context.Background(), pending.ID, pending.State, "code")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(broker, false)
	if err != nil {
		t.Fatal(err)
	}
	return handler, session
}
func submitIdentity(t *testing.T, handler http.Handler, sessionID string, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/identity/mutate", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.AddCookie(&http.Cookie{Name: "studio_session", Value: sessionID})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
func cloneValues(source url.Values) url.Values {
	target := url.Values{}
	for key, values := range source {
		target[key] = append([]string(nil), values...)
	}
	return target
}
