package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/drkliu/jimu-studio/internal/auth"
	"github.com/drkliu/jimu-studio/internal/provider"
)

type metadataReadOnlyIdentity struct{}

func (metadataReadOnlyIdentity) AuthorizationURL(request auth.AuthorizationRequest) string {
	return "https://identity.example/authorize?state=" + request.State
}

func (metadataReadOnlyIdentity) Exchange(_ context.Context, request auth.ExchangeRequest) (auth.Identity, error) {
	return auth.Identity{
		Subject: "reader", DisplayName: "Metadata reader",
		Roles: []string{"studio.metadata.read"}, AccessToken: request.Code + ".access",
		RefreshToken: request.Code + ".refresh", Expiry: time.Now().Add(time.Hour), Nonce: request.ExpectedNonce,
	}, nil
}

func (metadataReadOnlyIdentity) Refresh(context.Context, string) (auth.Token, error) {
	return auth.Token{}, nil
}

func TestMetadataWriteRequiresRoleBeforeProviderCall(t *testing.T) {
	t.Parallel()
	providerCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		providerCalls++
		http.Error(response, "unexpected provider call", http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	broker, err := auth.NewBroker(auth.Config{
		Tenants: []auth.Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: upstream.URL, Identity: metadataReadOnlyIdentity{}}},
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
	session, err := broker.Complete(context.Background(), pending.ID, pending.State, "metadata-code")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(broker, false)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{
		"csrf": {session.CSRF}, "code": {"orders"}, "name": {"Orders"},
		"expected_version": {"4"}, "field_count": {"0"},
	}
	request := httptest.NewRequest(http.MethodPost, "/metadata/edit", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.AddCookie(&http.Cookie{Name: "studio_session", Value: session.ID})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if providerCalls != 0 {
		t.Fatalf("provider calls=%d, want 0", providerCalls)
	}
}
