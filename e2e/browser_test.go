//go:build e2e

package e2e_test

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/drkliu/jimu-studio/internal/server"
)

func TestSecureAccessibleShellInBrowser(t *testing.T) {
	application := httptest.NewServer(server.New())
	t.Cleanup(application.Close)

	allocatorOptions := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	if os.Getenv("CI") == "true" {
		// GitHub-hosted runners disable the kernel features Chrome's sandbox requires.
		allocatorOptions = append(allocatorOptions, chromedp.NoSandbox)
	}
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	t.Cleanup(cancelAllocator)
	browser, cancelBrowser := chromedp.NewContext(allocator)
	t.Cleanup(cancelBrowser)
	ctx, cancelTimeout := context.WithTimeout(browser, 30*time.Second)
	t.Cleanup(cancelTimeout)

	var title string
	var landmarks int
	var browserStorageEntries int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(application.URL),
		chromedp.Title(&title),
		chromedp.Evaluate(`document.querySelectorAll('header, main').length`, &landmarks),
		chromedp.Evaluate(`localStorage.length + sessionStorage.length + document.cookie.length`, &browserStorageEntries),
	); err != nil {
		t.Fatalf("run browser smoke test: %v", err)
	}
	if title != "Jimu Studio" {
		t.Errorf("title = %q, want Jimu Studio", title)
	}
	if landmarks != 2 {
		t.Errorf("landmark count = %d, want 2", landmarks)
	}
	if browserStorageEntries != 0 {
		t.Errorf("browser storage entries = %d, want 0", browserStorageEntries)
	}
}
