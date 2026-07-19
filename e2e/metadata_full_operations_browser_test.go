//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/drkliu/jimu-studio/internal/auth"
	"github.com/drkliu/jimu-studio/internal/localprovider"
	"github.com/drkliu/jimu-studio/internal/provider"
	"github.com/drkliu/jimu-studio/internal/server"
)

type fullMetadataBrowserIdentity struct{ metadataBrowserIdentity }

func (identity fullMetadataBrowserIdentity) Exchange(ctx context.Context, request auth.ExchangeRequest) (auth.Identity, error) {
	result, err := identity.metadataBrowserIdentity.Exchange(ctx, request)
	result.Roles = append(result.Roles, "studio.audit.read")
	return result, err
}

func TestMetadataCreateDeleteAndAuditBrowserFlow(t *testing.T) {
	dsn := os.Getenv("JIMU_TEST_PG_DSN")
	if dsn == "" {
		t.Fatal("JIMU_TEST_PG_DSN is required for the PostgreSQL browser flow")
	}
	schema := fmt.Sprintf("jimu_studio_e2e_%d", time.Now().UnixNano())
	providerHandler, err := localprovider.NewHandler(context.Background(), localprovider.Config{DSN: dsn, Schema: schema})
	if err != nil {
		t.Fatalf("open PostgreSQL E2E provider: %v", err)
	}
	t.Cleanup(func() {
		if err := providerHandler.DropTestSchema(context.Background()); err != nil {
			t.Errorf("drop PostgreSQL E2E schema: %v", err)
		}
		_ = providerHandler.Close()
	})
	upstream := httptest.NewServer(providerHandler)
	t.Cleanup(upstream.Close)
	broker, err := auth.NewBroker(auth.Config{
		Tenants: []auth.Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: upstream.URL, Identity: fullMetadataBrowserIdentity{metadataBrowserIdentity{authorizeURL: "https://identity.example/authorize"}}}},
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
	session, err := broker.Complete(context.Background(), pending.ID, pending.State, "metadata-full-code")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := server.NewAuthenticated(broker, false)
	if err != nil {
		t.Fatal(err)
	}
	application := httptest.NewServer(handler)
	t.Cleanup(application.Close)

	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.Flag("disable-dev-shm-usage", true), chromedp.WSURLReadTimeout(60*time.Second))
	if os.Getenv("CI") == "true" {
		options = append(options, chromedp.NoSandbox)
	}
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	t.Cleanup(cancelAllocator)
	browser, cancelBrowser := chromedp.NewContext(allocator)
	t.Cleanup(cancelBrowser)
	ctx, cancel := context.WithTimeout(browser, 120*time.Second)
	t.Cleanup(cancel)

	var accessibilityViolations []string
	var deletedText, auditText string
	if err = chromedp.Run(ctx,
		network.Enable(),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return network.SetCookie("studio_session", session.ID).WithURL(application.URL).WithHTTPOnly(true).Do(ctx)
		}),
		chromedp.Navigate(application.URL+"/metadata/new"),
		chromedp.WaitVisible("#entity-code", chromedp.ByQuery),
		chromedp.SetValue("#entity-code", "customers", chromedp.ByQuery),
		chromedp.SetValue("#entity-name", "Customers", chromedp.ByQuery),
		chromedp.SetValue("#entity-kind", "standard", chromedp.ByQuery),
		chromedp.SetValue("#field-code-0", "id", chromedp.ByQuery),
		chromedp.SetValue("#field-type-0", "uuid", chromedp.ByQuery),
		chromedp.Click("[data-add-field]", chromedp.ByQuery),
		chromedp.WaitVisible("#field-code-1", chromedp.ByQuery),
		chromedp.SetValue("#field-code-1", "email", chromedp.ByQuery),
		chromedp.SetValue("#field-type-1", "string", chromedp.ByQuery),
		chromedp.Evaluate(`(() => { const failures=[]; if(document.documentElement.lang!=='en') failures.push('language'); if(document.querySelectorAll('h1').length!==1) failures.push('single-h1'); const ids=[...document.querySelectorAll('[id]')].map(e=>e.id); if(new Set(ids).size!==ids.length) failures.push('unique-ids'); for(const field of document.querySelectorAll('input:not([type=hidden]),select,textarea')) {if(!field.id||!document.querySelector('label[for="'+CSS.escape(field.id)+'"]')) failures.push('label:'+field.name);} for(const target of document.querySelectorAll('a,button,input:not([type=hidden]),select,textarea')) {const box=target.getBoundingClientRect();if(box.width<24||box.height<24) failures.push('target:'+target.tagName);} return failures;})()`, &accessibilityViolations),
		chromedp.Click(`//button[contains(., 'Create entity type')]`, chromedp.BySearch),
		chromedp.WaitVisible(`//code[contains(., 'customers')]`, chromedp.BySearch),
		chromedp.Click(`//article[.//code[contains(., 'customers')]]//a[contains(., 'Edit')]`, chromedp.BySearch),
		chromedp.WaitVisible(`//button[contains(., 'Review deletion impact')]`, chromedp.BySearch),
		chromedp.Click(`//button[contains(., 'Review deletion impact')]`, chromedp.BySearch),
		chromedp.WaitVisible("#delete-confirmation", chromedp.ByQuery),
		chromedp.SetValue("#delete-confirmation", "delete_entity", chromedp.ByQuery),
		chromedp.Click(`//button[contains(., 'Permanently delete entity')]`, chromedp.BySearch),
		chromedp.WaitVisible(`[role="alert"]`, chromedp.ByQuery),
		chromedp.SetValue("#delete-confirmation", provider.DeleteEntityConfirmation, chromedp.ByQuery),
		chromedp.Click(`//button[contains(., 'Permanently delete entity')]`, chromedp.BySearch),
		chromedp.WaitVisible(`//h1[contains(., 'Entity deleted')]`, chromedp.BySearch),
		chromedp.Text("body", &deletedText, chromedp.ByQuery),
		chromedp.Navigate(application.URL+"/audit"),
		chromedp.WaitVisible("table", chromedp.ByQuery),
		chromedp.Text("body", &auditText, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("metadata full-operation browser flow: %v", err)
	}
	if len(accessibilityViolations) != 0 {
		t.Fatalf("create editor accessibility checks failed: %v", accessibilityViolations)
	}
	if !strings.Contains(deletedText, "provider-authoritative record") || !strings.Contains(auditText, "studio.metadata.entities.create") || !strings.Contains(auditText, "studio.metadata.entities.delete") {
		t.Fatalf("deleted=%q audit=%q", deletedText, auditText)
	}
	if strings.Contains(deletedText+auditText, "metadata-full-code") {
		t.Fatal("token material rendered in full metadata operation flow")
	}
}
