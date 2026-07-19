package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/drkliu/jimu-studio/internal/auth"
	"github.com/drkliu/jimu-studio/internal/provider"
)

type migrationWriterIdentity struct{ metadataIdentity }

func (migrationWriterIdentity) Exchange(ctx context.Context, request auth.ExchangeRequest) (auth.Identity, error) {
	identity, err := (metadataIdentity{}).Exchange(ctx, request)
	identity.Roles = []string{"studio.metadata.read", "studio.metadata.write"}
	return identity, err
}

func TestMigrationApplyRequiresRoleBeforeDraftOrProvider(t *testing.T) {
	t.Parallel()
	providerCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		providerCalls++
		http.Error(response, "unexpected provider call", http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	broker, err := auth.NewBroker(auth.Config{
		Tenants: []auth.Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: upstream.URL, Identity: migrationWriterIdentity{}}},
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
	session, err := broker.Complete(context.Background(), pending.ID, pending.State, "code")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(broker, false)
	if err != nil {
		t.Fatal(err)
	}
	response := submitMigrationForm(t, handler, session.ID, "/metadata/apply", url.Values{
		"csrf": {session.CSRF}, "draft": {"opaque"}, "confirmation": {provider.ApplyMigrationConfirmation},
	})
	if response.Code != http.StatusForbidden || providerCalls != 0 {
		t.Fatalf("status=%d provider_calls=%d body=%s", response.Code, providerCalls, response.Body.String())
	}
}

func metadataWriterHandler(t *testing.T, upstream *httptest.Server) (http.Handler, auth.SessionResult) {
	t.Helper()
	broker, err := auth.NewBroker(auth.Config{
		Tenants: []auth.Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: upstream.URL, Identity: migrationWriterIdentity{}}},
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
	session, err := broker.Complete(context.Background(), pending.ID, pending.State, "code")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAuthenticated(broker, false)
	if err != nil {
		t.Fatal(err)
	}
	return handler, session
}
