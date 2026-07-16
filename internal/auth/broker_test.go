package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeIdentityProvider struct {
	mu             sync.Mutex
	authorizations int
	exchanges      int
	refreshes      int
}

func (provider *fakeIdentityProvider) AuthorizationURL(request AuthorizationRequest) string {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.authorizations++
	return "https://identity.example.test/authorize?state=" + request.State
}

func (provider *fakeIdentityProvider) Exchange(_ context.Context, request ExchangeRequest) (Identity, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.exchanges++
	if request.Code == "reject" {
		return Identity{}, errors.New("rejected code")
	}
	return Identity{
		Subject:      "user-1",
		DisplayName:  "Studio Operator",
		Roles:        []string{"studio.metadata.read"},
		AccessToken:  request.Code + "-access",
		RefreshToken: request.Code + "-refresh",
		Expiry:       time.Now().Add(time.Hour),
		Nonce:        request.ExpectedNonce,
	}, nil
}

func (provider *fakeIdentityProvider) Refresh(_ context.Context, refreshToken string) (Token, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.refreshes++
	return Token{AccessToken: refreshToken + "-new", RefreshToken: refreshToken, Expiry: time.Now().Add(time.Hour)}, nil
}

type fakeTenantClient struct {
	mu     sync.Mutex
	closed bool
	token  string
}

func (client *fakeTenantClient) Close() {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.closed = true
}

func (client *fakeTenantClient) isClosed() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.closed
}

func TestTenantSwitchInvalidatesOldSessionAndCreatesFreshClient(t *testing.T) {
	t.Parallel()

	identityA := &fakeIdentityProvider{}
	identityB := &fakeIdentityProvider{}
	var clients []*fakeTenantClient
	broker, err := NewBroker(Config{
		Tenants: []Tenant{
			{ID: "tenant-a", Name: "Tenant A", ProviderBaseURL: "https://a.example.test", Identity: identityA},
			{ID: "tenant-b", Name: "Tenant B", ProviderBaseURL: "https://b.example.test", Identity: identityB},
		},
		ClientFactory: func(_ context.Context, _ string, token string) (TenantClient, error) {
			client := &fakeTenantClient{token: token}
			clients = append(clients, client)
			return client, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	pendingA, err := broker.Begin("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	sessionA, err := broker.Complete(context.Background(), pendingA.ID, pendingA.State, "tenant-a-code")
	if err != nil {
		t.Fatal(err)
	}
	viewA, ok := broker.Session(context.Background(), sessionA.ID)
	if !ok || viewA.TenantID != "tenant-a" {
		t.Fatalf("tenant A session = %#v, %v", viewA, ok)
	}

	pendingB, err := broker.Switch(sessionA.ID, sessionA.CSRF, "tenant-b")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok = broker.Session(context.Background(), sessionA.ID); ok {
		t.Fatal("old tenant session survived switch")
	}
	if !clients[0].isClosed() {
		t.Fatal("old tenant provider client was not closed")
	}
	sessionB, err := broker.Complete(context.Background(), pendingB.ID, pendingB.State, "tenant-b-code")
	if err != nil {
		t.Fatal(err)
	}
	viewB, ok := broker.Session(context.Background(), sessionB.ID)
	if !ok || viewB.TenantID != "tenant-b" {
		t.Fatalf("tenant B session = %#v, %v", viewB, ok)
	}
	if len(clients) != 2 || clients[0] == clients[1] || clients[0].token == clients[1].token {
		t.Fatal("tenant switch reused provider client or token")
	}
	if identityA.authorizations != 1 || identityB.authorizations != 1 {
		t.Fatalf("authorization counts A=%d B=%d", identityA.authorizations, identityB.authorizations)
	}
}

func TestBrokerRejectsReplayAndBadCSRF(t *testing.T) {
	t.Parallel()

	identity := &fakeIdentityProvider{}
	broker, err := NewBroker(Config{
		Tenants: []Tenant{{ID: "tenant-a", Name: "Tenant A", ProviderBaseURL: "https://a.example.test", Identity: identity}},
		ClientFactory: func(_ context.Context, _ string, token string) (TenantClient, error) {
			return &fakeTenantClient{token: token}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := broker.Begin("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	session, err := broker.Complete(context.Background(), pending.ID, pending.State, "code")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = broker.Complete(context.Background(), pending.ID, pending.State, "code"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("replay error = %v, want ErrInvalidState", err)
	}
	if _, err = broker.Switch(session.ID, "wrong-csrf", "tenant-a"); !errors.Is(err, ErrInvalidCSRF) {
		t.Fatalf("switch error = %v, want ErrInvalidCSRF", err)
	}
	if _, ok := broker.Session(context.Background(), session.ID); !ok {
		t.Fatal("bad CSRF invalidated a valid session")
	}
}
