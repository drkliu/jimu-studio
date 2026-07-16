//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/drkliu/jimu-studio/internal/auth"
	"github.com/drkliu/jimu-studio/internal/provider"
	"github.com/drkliu/jimu-studio/internal/server"
)

type identityBrowserProvider struct{ authorizeURL string }

func (identity identityBrowserProvider) AuthorizationURL(request auth.AuthorizationRequest) string {
	values := url.Values{"state": {request.State}, "nonce": {request.Nonce}, "challenge": {request.CodeChallenge}}
	return identity.authorizeURL + "?" + values.Encode()
}
func (identityBrowserProvider) Exchange(_ context.Context, request auth.ExchangeRequest) (auth.Identity, error) {
	return auth.Identity{Subject: "admin", DisplayName: "Identity admin", Roles: []string{"identity.admin"}, AccessToken: request.Code + ".access", RefreshToken: request.Code + ".refresh", Expiry: time.Now().Add(time.Hour), Nonce: request.ExpectedNonce}, nil
}
func (identityBrowserProvider) Refresh(context.Context, string) (auth.Token, error) {
	return auth.Token{}, fmt.Errorf("unexpected refresh")
}

func TestIdentityStatusAndAssignmentBrowserFlow(t *testing.T) {
	var mu sync.Mutex
	user := provider.User{ID: "admin-1", DisplayName: "Admin One", Email: "admin@example.test", Status: "active", Roles: []string{"admin"}, Version: 3}
	roles := []provider.Role{{Key: "admin", DisplayName: "Administrator", System: true, Version: 1}, {Key: "operator", DisplayName: "Operator", Version: 2}}
	var statusConfirmation, statusKey, assignKey string
	var statusVersion, assignVersion int64
	unsafeProviderHeader := false
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		response.Header().Set("Content-Type", "application/json")
		if request.Header.Get("X-Tenant-ID") != "" || request.Header.Get("X-User-ID") != "" {
			unsafeProviderHeader = true
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/studio/v1/identity/users":
			_ = json.NewEncoder(response).Encode(provider.UserPage{Items: []provider.User{user}, NextCursor: "users-next"})
		case request.Method == http.MethodGet && request.URL.Path == "/studio/v1/identity/roles":
			_ = json.NewEncoder(response).Encode(provider.RolePage{Items: roles, NextCursor: "roles-next"})
		case request.Method == http.MethodPatch && strings.HasSuffix(request.URL.Path, "/status"):
			var body struct {
				Confirmation    string `json:"confirmation"`
				ExpectedVersion int64  `json:"expected_version"`
				IdempotencyKey  string `json:"idempotency_key"`
				Status          string `json:"status"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			statusConfirmation, statusVersion, statusKey = body.Confirmation, body.ExpectedVersion, body.IdempotencyKey
			user.Status = body.Status
			user.Version++
			_ = json.NewEncoder(response).Encode(user)
		case request.Method == http.MethodPut && strings.Contains(request.URL.Path, "/roles/operator"):
			var body struct {
				ExpectedVersion int64  `json:"expected_version"`
				IdempotencyKey  string `json:"idempotency_key"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			assignVersion, assignKey = body.ExpectedVersion, body.IdempotencyKey
			user.Roles = append(user.Roles, "operator")
			user.Version++
			_ = json.NewEncoder(response).Encode(user)
		default:
			http.Error(response, "unexpected identity request", http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)
	var applicationURL string
	identityServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		target := applicationURL + "/auth/callback?state=" + url.QueryEscape(query.Get("state")) + "&code=identity-code"
		http.Redirect(response, request, target, http.StatusSeeOther)
	}))
	t.Cleanup(identityServer.Close)
	broker, err := auth.NewBroker(auth.Config{Tenants: []auth.Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: upstream.URL, Identity: identityBrowserProvider{authorizeURL: identityServer.URL}}}, ClientFactory: func(ctx context.Context, baseURL, token string) (auth.TenantClient, error) {
		return provider.NewClient(ctx, baseURL, token, upstream.Client().Transport)
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := server.NewAuthenticated(broker, false)
	if err != nil {
		t.Fatal(err)
	}
	unsafeBrowserHeader := false
	application := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mu.Lock()
		if request.Header.Get("Authorization") != "" || request.Header.Get("X-Tenant-ID") != "" || request.Header.Get("X-User-ID") != "" {
			unsafeBrowserHeader = true
		}
		mu.Unlock()
		handler.ServeHTTP(response, request)
	}))
	applicationURL = application.URL
	t.Cleanup(application.Close)
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("disable-dev-shm-usage", true), chromedp.WSURLReadTimeout(60*time.Second))
	if os.Getenv("CI") == "true" {
		options = append(options, chromedp.NoSandbox)
	}
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	t.Cleanup(cancelAllocator)
	browser, cancelBrowser := chromedp.NewContext(allocator)
	t.Cleanup(cancelBrowser)
	ctx, cancel := context.WithTimeout(browser, 90*time.Second)
	t.Cleanup(cancel)
	var violations []string
	var credentialControls, storage int
	var bodyText string
	err = chromedp.Run(ctx, chromedp.Navigate(application.URL), chromedp.Click(`//button[contains(., 'Sign in to Alpha')]`, chromedp.BySearch), chromedp.Click("a.button", chromedp.ByQuery), chromedp.WaitVisible(`//strong[contains(., 'Alpha')]`, chromedp.BySearch), chromedp.Navigate(application.URL+"/identity"), chromedp.WaitVisible("#status-0", chromedp.ByQuery), chromedp.Evaluate(`document.querySelectorAll('input[type="password"],input[name*="token" i]').length`, &credentialControls), chromedp.Evaluate(`(() => { const failures=[]; if(document.querySelectorAll('h1').length!==1) failures.push('single-h1'); const ids=[...document.querySelectorAll('[id]')].map(e=>e.id); if(new Set(ids).size!==ids.length) failures.push('unique-ids'); for(const field of document.querySelectorAll('input:not([type=hidden]),select,textarea')) {if(!field.id||!document.querySelector('label[for="'+CSS.escape(field.id)+'"]')) failures.push('label:'+field.name);} for(const target of document.querySelectorAll('a,button,input:not([type=hidden]),select')){const box=target.getBoundingClientRect();if(box.width<24||box.height<24)failures.push('target:'+target.tagName);} return failures;})()`, &violations), chromedp.SetValue("#confirm-0", "disable_user", chromedp.ByQuery), chromedp.Click(`//button[contains(., 'Change status')]`, chromedp.BySearch), chromedp.WaitVisible(`[role="alert"]`, chromedp.ByQuery), chromedp.SetValue("#confirm-0", provider.DisableUserConfirmation, chromedp.ByQuery), chromedp.Click(`//button[contains(., 'Change status')]`, chromedp.BySearch), chromedp.WaitVisible(`//td[contains(., 'disabled')]`, chromedp.BySearch), chromedp.SetValue("#assign-0", "operator", chromedp.ByQuery), chromedp.Click(`//button[contains(., 'Assign role')]`, chromedp.BySearch), chromedp.WaitVisible(`//tr[th[contains(., 'admin-1')]]//code[contains(., 'operator')]`, chromedp.BySearch), chromedp.Evaluate(`localStorage.length+sessionStorage.length+document.cookie.length`, &storage), chromedp.Text("body", &bodyText, chromedp.ByQuery))
	if err != nil {
		t.Fatalf("identity browser flow: %v", err)
	}
	if len(violations) != 0 || credentialControls != 0 || storage != 0 {
		t.Fatalf("a11y=%v credentials=%d storage=%d", violations, credentialControls, storage)
	}
	if strings.Contains(bodyText, "identity-code") || !strings.Contains(bodyText, "System · immutable") {
		t.Fatalf("unsafe identity body: %q", bodyText)
	}
	mu.Lock()
	defer mu.Unlock()
	if unsafeBrowserHeader || unsafeProviderHeader {
		t.Fatal("trusted or bearer headers crossed browser/provider boundary")
	}
	if statusConfirmation != provider.DisableUserConfirmation || statusVersion != 3 || statusKey == "" || assignVersion != 4 || assignKey == "" {
		t.Fatalf("status=%q/%d/%q assign=%d/%q", statusConfirmation, statusVersion, statusKey, assignVersion, assignKey)
	}
}
