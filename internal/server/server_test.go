package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShellIsSecureAndAccessibleByDefault(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	New().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if got := response.Header.Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Fatalf("Content-Security-Policy = %q", got)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"<html lang=\"en\"", "<main", "<h1", "Jimu Studio"} {
		if !strings.Contains(string(body), required) {
			t.Errorf("shell missing %q", required)
		}
	}
}

func TestHealthEndpointIsBounded(t *testing.T) {
	t.Parallel()

	handler := New()
	for _, test := range []struct {
		method string
		want   int
	}{
		{method: http.MethodGet, want: http.StatusOK},
		{method: http.MethodPost, want: http.StatusMethodNotAllowed},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(test.method, "/healthz", nil))
		if recorder.Code != test.want {
			t.Errorf("%s /healthz status = %d, want %d", test.method, recorder.Code, test.want)
		}
	}
}
