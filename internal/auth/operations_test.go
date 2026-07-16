package auth

import (
	"context"
	"testing"
)

func TestSessionOperationAndStableDraftAreDisposedOnSwitch(t *testing.T) {
	t.Parallel()
	identity := &fakeIdentityProvider{}
	client := &fakeTenantClient{}
	broker, err := NewBroker(Config{
		Tenants: []Tenant{
			{ID: "alpha", Name: "Alpha", ProviderBaseURL: "https://alpha.example", Identity: identity},
			{ID: "beta", Name: "Beta", ProviderBaseURL: "https://beta.example", Identity: identity},
		},
		ClientFactory: func(context.Context, string, string) (TenantClient, error) { return client, nil },
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
	if err = broker.ValidateCSRF(session.ID, session.CSRF); err != nil {
		t.Fatalf("ValidateCSRF() = %v", err)
	}
	var received TenantClient
	if err = broker.WithClient(context.Background(), session.ID, func(value TenantClient) error { received = value; return nil }); err != nil || received != client {
		t.Fatalf("WithClient() client=%p error=%v", received, err)
	}
	first, err := broker.DraftKey(session.ID, "metadata:orders:4")
	if err != nil {
		t.Fatal(err)
	}
	second, err := broker.DraftKey(session.ID, "metadata:orders:4")
	if err != nil || first == "" || first != second {
		t.Fatalf("stable draft keys first=%q second=%q error=%v", first, second, err)
	}
	if _, err = broker.Switch(session.ID, session.CSRF, "beta"); err != nil {
		t.Fatal(err)
	}
	if _, err = broker.DraftKey(session.ID, "metadata:orders:4"); err == nil {
		t.Fatal("old tenant draft survived switch")
	}
	if err = broker.WithClient(context.Background(), session.ID, func(TenantClient) error { return nil }); err == nil {
		t.Fatal("old tenant client remained available")
	}
}

func TestDraftScopeIsBounded(t *testing.T) {
	t.Parallel()
	broker, err := NewBroker(Config{
		Tenants:       []Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: "https://alpha.example", Identity: &fakeIdentityProvider{}}},
		ClientFactory: func(context.Context, string, string) (TenantClient, error) { return &fakeTenantClient{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, _ := broker.Begin("alpha")
	session, _ := broker.Complete(context.Background(), pending.ID, pending.State, "code")
	if _, err = broker.DraftKey(session.ID, string(make([]byte, 1025))); err == nil {
		t.Fatal("DraftKey accepted an unbounded scope")
	}
}
