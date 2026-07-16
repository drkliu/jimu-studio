package auth

import (
	"context"
	"testing"
	"time"
)

type refreshIdentity struct{ now func() time.Time }

func (identity refreshIdentity) AuthorizationURL(request AuthorizationRequest) string {
	return "https://identity.example/authorize?state=" + request.State
}

func (identity refreshIdentity) Exchange(_ context.Context, request ExchangeRequest) (Identity, error) {
	return Identity{
		Subject: "subject", DisplayName: "Operator", AccessToken: request.Code + ".initial",
		RefreshToken: request.Code + ".refresh", Expiry: identity.now().Add(30 * time.Second), Nonce: request.ExpectedNonce,
	}, nil
}

func (identity refreshIdentity) Refresh(_ context.Context, value string) (Token, error) {
	return Token{AccessToken: value + ".rotated", RefreshToken: value + ".next", Expiry: identity.now().Add(time.Hour)}, nil
}

func TestSessionRefreshAtomicallyReplacesProviderClient(t *testing.T) {
	t.Parallel()
	now := time.Now()
	var clients []*fakeTenantClient
	broker, err := NewBroker(Config{
		Tenants: []Tenant{{ID: "alpha", Name: "Alpha", ProviderBaseURL: "https://provider.example", Identity: refreshIdentity{now: func() time.Time { return now }}}},
		ClientFactory: func(_ context.Context, _ string, token string) (TenantClient, error) {
			client := &fakeTenantClient{token: token}
			clients = append(clients, client)
			return client, nil
		},
		Now: func() time.Time { return now }, RefreshSkew: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := broker.Begin("alpha")
	if err != nil {
		t.Fatal(err)
	}
	session, err := broker.Complete(context.Background(), pending.ID, pending.State, "authorization-code")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := broker.Session(context.Background(), session.ID); !ok {
		t.Fatal("refreshed session unavailable")
	}
	if len(clients) != 2 || clients[0] == clients[1] || !clients[0].isClosed() || clients[1].isClosed() || clients[0].token == clients[1].token {
		t.Fatal("refresh did not atomically replace the token-bound provider client")
	}
}
