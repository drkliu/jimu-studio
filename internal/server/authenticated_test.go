package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drkliu/jimu-studio/internal/auth"
)

type shellIdentity struct{}

func (shellIdentity) AuthorizationURL(request auth.AuthorizationRequest) string {
	values := url.Values{"state": {request.State}, "nonce": {request.Nonce}, "challenge": {request.CodeChallenge}}
	return "https://identity.example/authorize?" + values.Encode()
}

func (shellIdentity) Exchange(_ context.Context, request auth.ExchangeRequest) (auth.Identity, error) {
	return auth.Identity{
		Subject: "subject", DisplayName: "Studio operator", Roles: []string{"studio.metadata.read"},
		AccessToken: request.Code + ".opaque", RefreshToken: request.Code + ".refresh",
		Expiry: time.Now().Add(time.Hour), Nonce: request.ExpectedNonce,
	}, nil
}

func (shellIdentity) Refresh(context.Context, string) (auth.Token, error) {
	return auth.Token{}, fmt.Errorf("unexpected refresh")
}

type shellClient struct {
	mu     sync.Mutex
	closed bool
}

func (client *shellClient) Close() {
	client.mu.Lock()
	client.closed = true
	client.mu.Unlock()
}

func TestAuthenticatedShellSwitchesWithFreshOpaqueSession(t *testing.T) {
	t.Parallel()
	var clients []*shellClient
	broker, err := auth.NewBroker(auth.Config{
		Tenants: []auth.Tenant{
			{ID: "alpha", Name: "Alpha", ProviderBaseURL: "https://alpha.example", Identity: shellIdentity{}},
			{ID: "beta", Name: "Beta", ProviderBaseURL: "https://beta.example", Identity: shellIdentity{}},
		},
		ClientFactory: func(context.Context, string, string) (auth.TenantClient, error) {
			client := &shellClient{}
			clients = append(clients, client)
			return client, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(broker, false)
	if err != nil {
		t.Fatal(err)
	}

	login := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader("tenant=alpha"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login.Header.Set("Sec-Fetch-Site", "same-origin")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d", loginResponse.Code)
	}
	pendingCookie := responseCookie(t, loginResponse, "studio_pending")
	if !pendingCookie.HttpOnly || pendingCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unsafe pending cookie: %#v", pendingCookie)
	}
	redirect, err := url.Parse(continuationURL(t, loginResponse.Body.String()))
	if err != nil {
		t.Fatal(err)
	}

	callback := httptest.NewRequest(http.MethodGet, "/auth/callback?code=first&state="+url.QueryEscape(redirect.Query().Get("state")), nil)
	callback.AddCookie(pendingCookie)
	callbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(callbackResponse, callback)
	if callbackResponse.Code != http.StatusSeeOther {
		t.Fatalf("callback status = %d body=%s", callbackResponse.Code, callbackResponse.Body.String())
	}
	sessionCookie := responseCookie(t, callbackResponse, "studio_session")
	if !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unsafe session cookie: %#v", sessionCookie)
	}

	index := httptest.NewRequest(http.MethodGet, "/", nil)
	index.AddCookie(sessionCookie)
	indexResponse := httptest.NewRecorder()
	handler.ServeHTTP(indexResponse, index)
	body := indexResponse.Body.String()
	if !strings.Contains(body, "Studio operator") || strings.Contains(body, "first.opaque") || strings.Contains(body, "first.refresh") {
		t.Fatalf("authenticated shell rendered unsafe or incomplete state: %s", body)
	}
	csrf := hiddenValue(t, body, "csrf")

	switchRequest := httptest.NewRequest(http.MethodPost, "/auth/switch", strings.NewReader(url.Values{"tenant": {"beta"}, "csrf": {csrf}}.Encode()))
	switchRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	switchRequest.Header.Set("Sec-Fetch-Site", "same-origin")
	switchRequest.AddCookie(sessionCookie)
	switchResponse := httptest.NewRecorder()
	handler.ServeHTTP(switchResponse, switchRequest)
	if switchResponse.Code != http.StatusOK {
		t.Fatalf("switch status = %d body=%s", switchResponse.Code, switchResponse.Body.String())
	}
	if _, ok := broker.Session(context.Background(), sessionCookie.Value); ok {
		t.Fatal("old tenant session remained active")
	}
	if len(clients) != 1 || !clients[0].closed {
		t.Fatal("old token-bound provider client was not closed before redirect")
	}
	if responseCookie(t, switchResponse, "studio_session").MaxAge != -1 {
		t.Fatal("switch did not clear the old browser session")
	}
}

func TestAuthenticatedShellRejectsCrossSiteMutationAndSetsHSTS(t *testing.T) {
	t.Parallel()
	broker, err := auth.NewBroker(auth.Config{
		Tenants:       []auth.Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: "https://alpha.example", Identity: shellIdentity{}}},
		ClientFactory: func(context.Context, string, string) (auth.TenantClient, error) { return &shellClient{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(broker, true)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader("tenant=alpha"))
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || response.Header().Get("Strict-Transport-Security") == "" {
		t.Fatalf("status=%d HSTS=%q", response.Code, response.Header().Get("Strict-Transport-Security"))
	}
}

func responseCookie(t *testing.T, response *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("response omitted cookie %q", name)
	return nil
}

func hiddenValue(t *testing.T, body, name string) string {
	t.Helper()
	marker := `name="` + name + `" value="`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("missing hidden input %q", name)
	}
	start += len(marker)
	end := strings.IndexByte(body[start:], '"')
	if end < 0 {
		t.Fatalf("unterminated hidden input %q", name)
	}
	return body[start : start+end]
}

func continuationURL(t *testing.T, body string) string {
	t.Helper()
	marker := `class="button" href="`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatal("missing authorization continuation link")
	}
	start += len(marker)
	end := strings.IndexByte(body[start:], '"')
	if end < 0 {
		t.Fatal("unterminated authorization continuation link")
	}
	return strings.ReplaceAll(body[start:start+end], "&amp;", "&")
}
