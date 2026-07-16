package auth

import (
	"context"
	"testing"
)

func TestVolatileDraftIsOpaqueAndDisposedOnSwitch(t *testing.T) {
	t.Parallel()
	identity := &fakeIdentityProvider{}
	broker, err := NewBroker(Config{
		Tenants: []Tenant{
			{ID: "alpha", Name: "Alpha", ProviderBaseURL: "https://alpha.example", Identity: identity},
			{ID: "beta", Name: "Beta", ProviderBaseURL: "https://beta.example", Identity: identity},
		},
		ClientFactory: func(context.Context, string, string) (TenantClient, error) { return &fakeTenantClient{}, nil },
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
	reference, err := broker.StoreDraft(session.ID, "migration", struct{ Code string }{Code: "orders"})
	if err != nil || reference == "" || reference == "orders" {
		t.Fatalf("StoreDraft() reference=%q error=%v", reference, err)
	}
	value, ok := broker.LoadDraft(session.ID, "migration", reference)
	if !ok || value.(struct{ Code string }).Code != "orders" {
		t.Fatalf("LoadDraft() value=%#v ok=%v", value, ok)
	}
	if _, err = broker.Switch(session.ID, session.CSRF, "beta"); err != nil {
		t.Fatal(err)
	}
	if _, ok = broker.LoadDraft(session.ID, "migration", reference); ok {
		t.Fatal("migration draft survived tenant switch")
	}
}

func TestVolatileDraftRejectsUnsafeCategoryAndNil(t *testing.T) {
	t.Parallel()
	broker, session := operationBroker(t)
	if _, err := broker.StoreDraft(session.ID, "../migration", "value"); err == nil {
		t.Fatal("unsafe draft category accepted")
	}
	if _, err := broker.StoreDraft(session.ID, "migration", nil); err == nil {
		t.Fatal("nil draft accepted")
	}
}

func operationBroker(t *testing.T) (*Broker, SessionResult) {
	t.Helper()
	broker, err := NewBroker(Config{
		Tenants:       []Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: "https://alpha.example", Identity: &fakeIdentityProvider{}}},
		ClientFactory: func(context.Context, string, string) (TenantClient, error) { return &fakeTenantClient{}, nil },
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
	return broker, session
}
