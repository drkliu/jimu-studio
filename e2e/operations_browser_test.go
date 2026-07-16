//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/drkliu/jimu-studio/internal/auth"
	"github.com/drkliu/jimu-studio/internal/provider"
	"github.com/drkliu/jimu-studio/internal/server"
)

type operationsBrowserIdentity struct{}

func (operationsBrowserIdentity) AuthorizationURL(auth.AuthorizationRequest) string {
	return "https://identity.example/authorize"
}

func (operationsBrowserIdentity) Exchange(_ context.Context, request auth.ExchangeRequest) (auth.Identity, error) {
	return auth.Identity{
		Subject: "operator", DisplayName: "Operations operator",
		Roles: []string{"studio.audit.read", "studio.quota.admin", "studio.quota.read", "studio.workflow.operate", "studio.workflow.read"},
		AccessToken: "operations.access", RefreshToken: "operations.refresh",
		Expiry: time.Now().Add(time.Hour), Nonce: request.ExpectedNonce,
	}, nil
}

func (operationsBrowserIdentity) Refresh(context.Context, string) (auth.Token, error) {
	return auth.Token{}, nil
}

func TestOperationsConsoleIsAccessibleBoundedAndRedacted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.Header.Get("Authorization"), "Bearer ") {
			http.Error(response, "missing bearer", http.StatusUnauthorized)
			return
		}
		if request.URL.Query().Get("limit") != "50" {
			http.Error(response, "unbounded query", http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/studio/v1/workflows/runs":
			_, _ = response.Write([]byte(`{"items":[{"id":"run-1","workflow":"checkout","state":"running","version":7,"created_at":"2026-07-16T10:00:00Z","updated_at":"2026-07-16T10:01:00Z","lease_state":"active"}]}`))
		case "/studio/v1/workflows/runs/run-1/tasks":
			_, _ = response.Write([]byte(`{"items":[{"id":"task-1","run_id":"run-1","code":"charge","state":"failed","attempt":2,"version":9,"error_code":"provider_timeout","lease_state":"expired","recovery_state":"lease_expired"}]}`))
		case "/studio/v1/quota/plans":
			_, _ = response.Write([]byte(`{"items":[{"code":"standard","version":3,"effective_at":"2026-08-01T00:00:00Z","window_seconds":60,"limits":{"requests":100}}]}`))
		case "/studio/v1/quota/usage":
			_, _ = response.Write([]byte(`{"items":[{"route":"checkout","unit":"request","quantity":12,"occurred_at":"2026-07-16T10:02:00Z","plan_code":"standard","plan_version":3}]}`))
		case "/studio/v1/quota/discrepancies":
			_, _ = response.Write([]byte(`{"items":[{"id":"difference-1","route":"checkout","unit":"request","from":"2026-07-16T10:00:00Z","until":"2026-07-16T10:05:00Z","enforcement_total":12,"ledger_total":11,"plan_code":"standard","plan_version":3}]}`))
		case "/studio/v1/audit":
			_, _ = response.Write([]byte(`{"items":[{"id":"audit-1","actor_user_id":"operator","action":"workflow.retry","target_type":"task","target_id":"task-1","occurred_at":"2026-07-16T10:03:00Z","details":{"secret":"must-not-render"},"redacted_paths":["details.secret"]}]}`))
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(upstream.Close)

	broker, err := auth.NewBroker(auth.Config{
		Tenants: []auth.Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: upstream.URL, Identity: operationsBrowserIdentity{}}},
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
	handler, err := server.NewAuthenticated(broker, false)
	if err != nil {
		t.Fatal(err)
	}
	application := httptest.NewServer(handler)
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

	var workflowText string
	var quotaText string
	var auditText string
	var violations []string
	if err = chromedp.Run(ctx,
		network.Enable(),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return network.SetCookie("studio_session", session.ID).WithURL(application.URL).WithHTTPOnly(true).Do(ctx)
		}),
		chromedp.Navigate(application.URL+"/workflows"),
		chromedp.WaitVisible("table", chromedp.ByQuery),
		chromedp.Text("body", &workflowText, chromedp.ByQuery),
		chromedp.Navigate(application.URL+"/quota"),
		chromedp.WaitVisible("table", chromedp.ByQuery),
		chromedp.Text("body", &quotaText, chromedp.ByQuery),
		chromedp.Navigate(application.URL+"/audit"),
		chromedp.WaitVisible("table", chromedp.ByQuery),
		chromedp.Text("body", &auditText, chromedp.ByQuery),
		chromedp.Evaluate(`(() => { const failures=[]; if(document.documentElement.lang !== 'en') failures.push('language'); if(document.querySelectorAll('h1').length !== 1) failures.push('single-h1'); const ids=[...document.querySelectorAll('[id]')].map(e=>e.id); if(new Set(ids).size !== ids.length) failures.push('unique-ids'); for(const field of document.querySelectorAll('input:not([type=hidden]),select,textarea')) { if(!field.id || !document.querySelector('label[for="'+CSS.escape(field.id)+'"]')) failures.push('label:'+field.name); } return failures; })()`, &violations),
	); err != nil {
		t.Fatalf("run operations browser flow: %v", err)
	}
	for label, result := range map[string]struct {
		text     string
		required []string
	}{
		"workflow": {workflowText, []string{"checkout", "charge", "provider_timeout", "lease_expired", provider.CancelRunConfirmation, provider.RetryTaskConfirmation}},
		"quota":    {quotaText, []string{"standard", "observational", "difference-1", provider.PublishPlanConfirmation}},
		"audit":    {auditText, []string{"workflow.retry", "details.secret"}},
	} {
		for _, required := range result.required {
			if !strings.Contains(result.text, required) {
				t.Errorf("%s page omitted %q", label, required)
			}
		}
	}
	if strings.Contains(auditText, "must-not-render") || strings.Contains(workflowText+quotaText+auditText, "operations.access") {
		t.Fatal("operations UI rendered sensitive provider or token data")
	}
	if len(violations) != 0 {
		t.Fatalf("operations accessibility checks failed: %v", violations)
	}
}
