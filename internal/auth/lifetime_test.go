package auth

import (
	"context"
	"testing"
)

func TestProviderClientLifetimeIsDetachedFromLoginRequest(t *testing.T) {
	requestContext, cancelRequest := context.WithCancel(context.Background())
	var clientContext context.Context
	broker, err := NewBroker(Config{
		Tenants: []Tenant{{
			ID: "alpha", Name: "Alpha", ProviderBaseURL: "https://alpha.example", Identity: &fakeIdentityProvider{},
		}},
		ClientFactory: func(ctx context.Context, _, _ string) (TenantClient, error) {
			clientContext = ctx
			return &fakeTenantClient{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := broker.Begin("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = broker.Complete(requestContext, pending.ID, pending.State, "code"); err != nil {
		t.Fatal(err)
	}
	cancelRequest()
	select {
	case <-clientContext.Done():
		t.Fatal("provider client inherited cancellation from the login request")
	default:
	}
}
