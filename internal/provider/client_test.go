package provider

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientRejectsUnsafeConfiguration(t *testing.T) {
	t.Parallel()
	opaque := testOpaque(t)

	for _, test := range []struct {
		name    string
		baseURL string
		token   string
	}{
		{name: "credentials in URL", baseURL: "https://user:secret@example.test", token: opaque},
		{name: "non HTTP URL", baseURL: "file:///tmp/provider", token: opaque},
		{name: "empty token", baseURL: "https://example.test", token: ""},
		{name: "header injection", baseURL: "https://example.test", token: "safe\r\ntoken"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewClient(context.Background(), test.baseURL, test.token, nil); err == nil {
				t.Fatal("NewClient accepted unsafe configuration")
			}
		})
	}
}

func TestClientSendsBearerOnlyAndRejectsRedirects(t *testing.T) {
	t.Parallel()
	opaque := testOpaque(t)

	redirectTargetCalled := false
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectTargetCalled = true
	}))
	t.Cleanup(redirectTarget.Close)

	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+opaque {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", request.Header.Get("Accept"))
		}
		for _, forbidden := range []string{"X-Jimu-Tenant-ID", "X-Jimu-User-ID", "X-Tenant-ID", "X-User-ID"} {
			if value := request.Header.Get(forbidden); value != "" {
				t.Errorf("forbidden header %s = %q", forbidden, value)
			}
		}
		http.Redirect(response, request, redirectTarget.URL, http.StatusFound)
	}))
	t.Cleanup(provider.Close)

	client, err := NewClient(context.Background(), provider.URL, opaque, provider.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	response, err := client.do(context.Background(), http.MethodGet, "/probe", nil)
	if !errors.Is(err, ErrRedirect) {
		t.Fatalf("do error = %v, want ErrRedirect", err)
	}
	if response != nil {
		response.Body.Close()
	}
	if redirectTargetCalled {
		t.Fatal("provider client followed a redirect with bearer credentials")
	}
}

func TestClientCloseCancelsInFlightRequests(t *testing.T) {
	t.Parallel()

	opaque := testOpaque(t)
	started := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	}))
	t.Cleanup(provider.Close)

	client, err := NewClient(context.Background(), provider.URL, opaque, provider.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, requestErr := client.do(context.Background(), http.MethodGet, "/probe", nil)
		done <- requestErr
	}()
	<-started
	client.Close()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("request error = %v, want context.Canceled", err)
	}
}

func testOpaque(t *testing.T) string {
	t.Helper()
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(value)
}
