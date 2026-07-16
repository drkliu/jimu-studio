package auth

import (
	"context"
	"testing"
)

type unsafeAuthorizationIdentity struct{ *fakeIdentityProvider }

func (unsafeAuthorizationIdentity) AuthorizationURL(AuthorizationRequest) string {
	return "javascript:location.assign('https://attacker.example')"
}

func TestBeginRejectsUnsafeAuthorizationNavigation(t *testing.T) {
	t.Parallel()
	broker, err := NewBroker(Config{
		Tenants:       []Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: "https://provider.example", Identity: unsafeAuthorizationIdentity{&fakeIdentityProvider{}}}},
		ClientFactory: func(context.Context, string, string) (TenantClient, error) { return &fakeTenantClient{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = broker.Begin("alpha"); err == nil {
		t.Fatal("Begin accepted a non-HTTP authorization URL")
	}
}
