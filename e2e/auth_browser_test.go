//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/drkliu/jimu-studio/internal/auth"
	"github.com/drkliu/jimu-studio/internal/server"
)

type browserIdentity struct {
	tenant       string
	authorizeURL string
}

func (identity browserIdentity) AuthorizationURL(request auth.AuthorizationRequest) string {
	query := url.Values{
		"tenant": {identity.tenant}, "state": {request.State}, "nonce": {request.Nonce},
		"challenge": {request.CodeChallenge},
	}
	return identity.authorizeURL + "?" + query.Encode()
}

func (identity browserIdentity) Exchange(_ context.Context, request auth.ExchangeRequest) (auth.Identity, error) {
	return auth.Identity{
		Subject: identity.tenant + "-operator", DisplayName: identity.tenant + " operator",
		Roles: []string{"studio.metadata.read"}, AccessToken: request.Code + ".access",
		RefreshToken: request.Code + ".refresh", Expiry: time.Now().Add(time.Hour), Nonce: request.ExpectedNonce,
	}, nil
}

func (browserIdentity) Refresh(context.Context, string) (auth.Token, error) {
	return auth.Token{}, fmt.Errorf("unexpected refresh")
}

type browserClient struct {
	mu     sync.Mutex
	closed bool
}

func (client *browserClient) Close() {
	client.mu.Lock()
	client.closed = true
	client.mu.Unlock()
}

func (client *browserClient) isClosed() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.closed
}

func TestTenantSwitchReauthenticatesWithoutBrowserTokens(t *testing.T) {
	var applicationURL string
	identityServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("state") == "" || query.Get("nonce") == "" || query.Get("challenge") == "" {
			http.Error(response, "missing OIDC proof", http.StatusBadRequest)
			return
		}
		target := applicationURL + "/auth/callback?state=" + url.QueryEscape(query.Get("state")) + "&code=" + url.QueryEscape(query.Get("tenant")+"-code")
		http.Redirect(response, request, target, http.StatusSeeOther)
	}))
	t.Cleanup(identityServer.Close)

	var stateMu sync.Mutex
	var clients []*browserClient
	broker, err := auth.NewBroker(auth.Config{
		Tenants: []auth.Tenant{
			{ID: "alpha", Name: "Alpha", ProviderBaseURL: "https://alpha.provider.example", Identity: browserIdentity{tenant: "alpha", authorizeURL: identityServer.URL}},
			{ID: "beta", Name: "Beta", ProviderBaseURL: "https://beta.provider.example", Identity: browserIdentity{tenant: "beta", authorizeURL: identityServer.URL}},
		},
		ClientFactory: func(context.Context, string, string) (auth.TenantClient, error) {
			client := &browserClient{}
			stateMu.Lock()
			clients = append(clients, client)
			stateMu.Unlock()
			return client, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := server.NewAuthenticated(broker, false)
	if err != nil {
		t.Fatal(err)
	}
	var unsafeBrowserHeader bool
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

	var oldSession string
	var storage int
	var skipLinkFocused bool
	var accessibilityViolations []string
	if err = chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(application.URL),
		chromedp.Focus(".skip-link", chromedp.ByQuery),
		chromedp.Evaluate(`document.activeElement.matches('.skip-link')`, &skipLinkFocused),
		chromedp.Click(`//button[contains(., 'Sign in to Alpha')]`, chromedp.BySearch),
		chromedp.Click("a.button", chromedp.ByQuery),
		chromedp.WaitVisible(`//strong[contains(., 'Alpha')]`, chromedp.BySearch),
		chromedp.ActionFunc(func(ctx context.Context) error {
			cookies, err := network.GetCookies().WithURLs([]string{application.URL}).Do(ctx)
			if err != nil {
				return err
			}
			for _, cookie := range cookies {
				if cookie.Name == "studio_session" {
					oldSession = cookie.Value
				}
			}
			return nil
		}),
		chromedp.SetValue("#tenant", "beta", chromedp.ByQuery),
		chromedp.Click(`//button[contains(., 'Switch and sign in')]`, chromedp.BySearch),
		chromedp.Click("a.button", chromedp.ByQuery),
		chromedp.WaitVisible(`//strong[contains(., 'Beta')]`, chromedp.BySearch),
		chromedp.Evaluate(`localStorage.length + sessionStorage.length + document.cookie.length`, &storage),
		chromedp.Evaluate(`(() => { const failures=[]; if(document.documentElement.lang !== 'en') failures.push('language'); if(document.querySelectorAll('h1').length !== 1) failures.push('single-h1'); const ids=[...document.querySelectorAll('[id]')].map(e=>e.id); if(new Set(ids).size !== ids.length) failures.push('unique-ids'); for(const field of document.querySelectorAll('input:not([type=hidden]),select,textarea')) { if(!field.id || !document.querySelector('label[for="'+CSS.escape(field.id)+'"]')) failures.push('label:'+field.name); } for(const target of document.querySelectorAll('a,button,input:not([type=hidden]),select,textarea')) { const box=target.getBoundingClientRect(); if(box.width < 24 || box.height < 24) failures.push('target:'+target.tagName); } if(document.querySelector('[tabindex]:not([tabindex="-1"]):not([tabindex="0"])')) failures.push('tab-order'); return failures; })()`, &accessibilityViolations),
	); err != nil {
		t.Fatalf("run tenant-switch browser flow: %v", err)
	}
	if oldSession == "" {
		t.Fatal("browser omitted opaque HttpOnly session")
	}
	if _, ok := broker.Session(context.Background(), oldSession); ok {
		t.Fatal("old tenant session remained valid after browser switch")
	}
	stateMu.Lock()
	clientCount := len(clients)
	clientsFresh := clientCount == 2 && clients[0].isClosed() && !clients[1].isClosed()
	unsafeHeadersObserved := unsafeBrowserHeader
	stateMu.Unlock()
	if !clientsFresh {
		t.Fatalf("provider-client lifecycle is not fresh: count=%d", clientCount)
	}
	if unsafeHeadersObserved {
		t.Fatal("browser sent bearer or trusted identity headers")
	}
	if storage != 0 {
		t.Fatalf("script-readable browser storage length = %d", storage)
	}
	if !skipLinkFocused {
		t.Fatal("skip link could not receive keyboard focus")
	}
	if len(accessibilityViolations) != 0 {
		t.Fatalf("accessibility checks failed: %v", accessibilityViolations)
	}
	var text string
	if err = chromedp.Run(ctx, chromedp.Text("body", &text, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "alpha-code") || strings.Contains(text, "beta-code") {
		t.Fatal("token material was rendered in the browser")
	}
}
