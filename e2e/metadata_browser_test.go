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

type metadataBrowserIdentity struct{ authorizeURL string }

func (identity metadataBrowserIdentity) AuthorizationURL(request auth.AuthorizationRequest) string {
	query := url.Values{"state": {request.State}, "nonce": {request.Nonce}, "challenge": {request.CodeChallenge}}
	return identity.authorizeURL + "?" + query.Encode()
}

func (metadataBrowserIdentity) Exchange(_ context.Context, request auth.ExchangeRequest) (auth.Identity, error) {
	return auth.Identity{
		Subject: "operator", DisplayName: "Metadata operator",
		Roles: []string{"studio.metadata.read", "studio.metadata.write"}, AccessToken: request.Code + ".access",
		RefreshToken: request.Code + ".refresh", Expiry: time.Now().Add(time.Hour), Nonce: request.ExpectedNonce,
	}, nil
}

func (metadataBrowserIdentity) Refresh(context.Context, string) (auth.Token, error) {
	return auth.Token{}, fmt.Errorf("unexpected refresh")
}

func TestMetadataEditConflictAndAccessibilityFlow(t *testing.T) {
	var stateMu sync.Mutex
	entity := provider.Entity{
		Code: "orders", Name: "Orders", Kind: "standard", Version: 4,
		Fields: []provider.Field{{Code: "id", DataType: "uuid", Required: true, ReadOnly: true}},
	}
	conflictPending := true
	var expectedVersions []int64
	var idempotencyKeys []string
	providerAuthorization := true
	providerTrustedHeader := false
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		stateMu.Lock()
		defer stateMu.Unlock()
		if !strings.HasPrefix(request.Header.Get("Authorization"), "Bearer ") {
			providerAuthorization = false
		}
		if request.Header.Get("X-Tenant-ID") != "" || request.Header.Get("X-User-ID") != "" {
			providerTrustedHeader = true
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case http.MethodGet:
			_ = json.NewEncoder(response).Encode(provider.EntityPage{Items: []provider.Entity{entity}, NextCursor: "page-2"})
		case http.MethodPut:
			var mutation struct {
				Entity          provider.Entity `json:"entity"`
				ExpectedVersion int64           `json:"expected_version"`
				IdempotencyKey  string          `json:"idempotency_key"`
			}
			if err := json.NewDecoder(request.Body).Decode(&mutation); err != nil {
				http.Error(response, "invalid mutation", http.StatusBadRequest)
				return
			}
			expectedVersions = append(expectedVersions, mutation.ExpectedVersion)
			idempotencyKeys = append(idempotencyKeys, mutation.IdempotencyKey)
			if conflictPending {
				conflictPending = false
				entity.Name = "Orders current"
				entity.Version = 5
				response.WriteHeader(http.StatusConflict)
				_, _ = response.Write([]byte(`{"code":"version_conflict","message":"entity changed","request_id":"browser-conflict"}`))
				return
			}
			entity = mutation.Entity
			entity.Version = 6
			_ = json.NewEncoder(response).Encode(entity)
		default:
			http.Error(response, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(upstream.Close)

	var applicationURL string
	identityServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("state") == "" || query.Get("nonce") == "" || query.Get("challenge") == "" {
			http.Error(response, "missing OIDC proof", http.StatusBadRequest)
			return
		}
		target := applicationURL + "/auth/callback?state=" + url.QueryEscape(query.Get("state")) + "&code=metadata-code"
		http.Redirect(response, request, target, http.StatusSeeOther)
	}))
	t.Cleanup(identityServer.Close)

	broker, err := auth.NewBroker(auth.Config{
		Tenants: []auth.Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: upstream.URL, Identity: metadataBrowserIdentity{authorizeURL: identityServer.URL}}},
		ClientFactory: func(ctx context.Context, baseURL, token string) (auth.TenantClient, error) {
			return provider.NewClient(ctx, baseURL, token, upstream.Client().Transport)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := server.NewAuthenticated(broker, false)
	if err != nil {
		t.Fatal(err)
	}
	unsafeBrowserHeader := false
	application := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		stateMu.Lock()
		if request.Header.Get("Authorization") != "" || request.Header.Get("X-Tenant-ID") != "" || request.Header.Get("X-User-ID") != "" {
			unsafeBrowserHeader = true
		}
		stateMu.Unlock()
		handler.ServeHTTP(response, request)
	}))
	applicationURL = application.URL
	t.Cleanup(application.Close)

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("disable-dev-shm-usage", true), chromedp.WSURLReadTimeout(60*time.Second))
	if os.Getenv("CI") == "true" {
		allocatorOptions = append(allocatorOptions, chromedp.NoSandbox)
	}
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	t.Cleanup(cancelAllocator)
	browser, cancelBrowser := chromedp.NewContext(allocator)
	t.Cleanup(cancelBrowser)
	ctx, cancelTimeout := context.WithTimeout(browser, 90*time.Second)
	t.Cleanup(cancelTimeout)

	var dirtyGuarded bool
	var accessibilityViolations []string
	var browserStorage int
	var bodyText string
	if err = chromedp.Run(ctx,
		chromedp.Navigate(application.URL),
		chromedp.Click(`//button[contains(., 'Sign in to Alpha')]`, chromedp.BySearch),
		chromedp.Click("a.button", chromedp.ByQuery),
		chromedp.WaitVisible(`//strong[contains(., 'Alpha')]`, chromedp.BySearch),
		chromedp.Navigate(application.URL+"/metadata"),
		chromedp.WaitVisible("table", chromedp.ByQuery),
		chromedp.Click(".entity-card a", chromedp.ByQuery),
		chromedp.WaitVisible("#entity-name", chromedp.ByQuery),
		chromedp.SetValue("#entity-name", "Orders submitted", chromedp.ByQuery),
		chromedp.Evaluate(`(() => { const event = new Event('beforeunload', {cancelable:true}); window.dispatchEvent(event); return event.defaultPrevented; })()`, &dirtyGuarded),
		chromedp.Evaluate(`(() => { const failures=[]; if(document.documentElement.lang !== 'en') failures.push('language'); if(document.querySelectorAll('h1').length !== 1) failures.push('single-h1'); const ids=[...document.querySelectorAll('[id]')].map(e=>e.id); if(new Set(ids).size !== ids.length) failures.push('unique-ids'); for(const field of document.querySelectorAll('input:not([type=hidden]),select,textarea')) { if(!field.id || !document.querySelector('label[for="'+CSS.escape(field.id)+'"]')) failures.push('label:'+field.name); } for(const target of document.querySelectorAll('a,button,input:not([type=hidden]),select,textarea')) { const box=target.getBoundingClientRect(); if(box.width < 24 || box.height < 24) failures.push('target:'+target.tagName+':'+target.textContent.trim()+':'+box.width+'x'+box.height); } return failures; })()`, &accessibilityViolations),
		chromedp.Click(`//button[contains(., 'Save guarded changes')]`, chromedp.BySearch),
		chromedp.WaitVisible(`//h1[contains(., 'Version conflict')]`, chromedp.BySearch),
		chromedp.WaitVisible(`//code[contains(., 'browser-conflict')]`, chromedp.BySearch),
		chromedp.Click(`//a[contains(., 'Edit the current version')]`, chromedp.BySearch),
		chromedp.WaitVisible("#entity-name", chromedp.ByQuery),
		chromedp.SetValue("#entity-name", "Orders reconciled", chromedp.ByQuery),
		chromedp.Click(`//button[contains(., 'Save guarded changes')]`, chromedp.BySearch),
		chromedp.WaitVisible(`[role="status"]`, chromedp.ByQuery),
		chromedp.Evaluate(`localStorage.length + sessionStorage.length + document.cookie.length`, &browserStorage),
		chromedp.Text("body", &bodyText, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("run metadata browser flow: %v", err)
	}
	if !dirtyGuarded {
		t.Fatal("dirty metadata form did not install a navigation guard")
	}
	if len(accessibilityViolations) != 0 {
		t.Fatalf("metadata accessibility checks failed: %v", accessibilityViolations)
	}
	if browserStorage != 0 {
		t.Fatalf("script-readable browser storage length=%d", browserStorage)
	}
	if strings.Contains(bodyText, "metadata-code") {
		t.Fatal("token material was rendered in metadata UI")
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	if unsafeBrowserHeader || providerTrustedHeader || !providerAuthorization {
		t.Fatalf("unsafe header boundary: browser=%v provider_trusted=%v provider_authorized=%v", unsafeBrowserHeader, providerTrustedHeader, providerAuthorization)
	}
	if len(expectedVersions) != 2 || expectedVersions[0] != 4 || expectedVersions[1] != 5 {
		t.Fatalf("expected versions=%v", expectedVersions)
	}
	if len(idempotencyKeys) != 2 || idempotencyKeys[0] == "" || idempotencyKeys[1] == "" {
		t.Fatalf("idempotency keys=%v", idempotencyKeys)
	}
}
