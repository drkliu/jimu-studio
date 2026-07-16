package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"
)

// NewBroker validates tenant registrations and constructs an empty in-memory broker.
func NewBroker(config Config) (*Broker, error) {
	if len(config.Tenants) == 0 || config.ClientFactory == nil {
		return nil, errors.New("at least one tenant and a provider client factory are required")
	}
	if config.PendingTTL == 0 {
		config.PendingTTL = 10 * time.Minute
	}
	if config.SessionTTL == 0 {
		config.SessionTTL = 8 * time.Hour
	}
	if config.RefreshSkew == 0 {
		config.RefreshSkew = time.Minute
	}
	if config.MaxPending == 0 {
		config.MaxPending = 1024
	}
	if config.MaxSessions == 0 {
		config.MaxSessions = 4096
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = randomValue
	}
	if config.PendingTTL <= 0 || config.SessionTTL <= 0 || config.RefreshSkew < 0 || config.MaxPending < 1 || config.MaxSessions < 1 {
		return nil, errors.New("authentication bounds must be positive")
	}

	broker := &Broker{
		tenants:       make(map[string]Tenant, len(config.Tenants)),
		pending:       make(map[string]pendingState),
		sessions:      make(map[string]*sessionState),
		clientFactory: config.ClientFactory,
		pendingTTL:    config.PendingTTL,
		sessionTTL:    config.SessionTTL,
		refreshSkew:   config.RefreshSkew,
		maxPending:    config.MaxPending,
		maxSessions:   config.MaxSessions,
		now:           config.Now,
		random:        config.Random,
	}
	for _, tenant := range config.Tenants {
		if tenant.ID == "" || tenant.Name == "" || tenant.ProviderBaseURL == "" || tenant.Identity == nil {
			return nil, errors.New("tenant ID, name, provider URL, and identity provider are required")
		}
		if _, exists := broker.tenants[tenant.ID]; exists {
			return nil, fmt.Errorf("duplicate tenant %q", tenant.ID)
		}
		broker.tenants[tenant.ID] = tenant
		broker.tenantViews = append(broker.tenantViews, TenantView{ID: tenant.ID, Name: tenant.Name})
	}
	sort.Slice(broker.tenantViews, func(left, right int) bool { return broker.tenantViews[left].ID < broker.tenantViews[right].ID })
	return broker, nil
}

// Tenants returns an immutable copy of the configured browser-safe tenant list.
func (broker *Broker) Tenants() []TenantView {
	return append([]TenantView(nil), broker.tenantViews...)
}

// Begin creates single-use state, nonce, and PKCE values for a tenant authorization.
func (broker *Broker) Begin(tenantID string) (Pending, error) {
	tenant, exists := broker.tenants[tenantID]
	if !exists {
		return Pending{}, ErrUnknownTenant
	}
	id, err := broker.random(32)
	if err != nil {
		return Pending{}, fmt.Errorf("create pending ID: %w", err)
	}
	state, err := broker.random(32)
	if err != nil {
		return Pending{}, fmt.Errorf("create OIDC state: %w", err)
	}
	nonce, err := broker.random(32)
	if err != nil {
		return Pending{}, fmt.Errorf("create OIDC nonce: %w", err)
	}
	verifier, err := broker.random(32)
	if err != nil {
		return Pending{}, fmt.Errorf("create PKCE verifier: %w", err)
	}
	challengeHash := sha256.Sum256([]byte(verifier))
	request := AuthorizationRequest{
		State:         state,
		Nonce:         nonce,
		CodeChallenge: base64.RawURLEncoding.EncodeToString(challengeHash[:]),
	}
	authorizationURL := tenant.Identity.AuthorizationURL(request)
	parsedAuthorizationURL, err := url.Parse(authorizationURL)
	if err != nil || len(authorizationURL) > 16*1024 || (parsedAuthorizationURL.Scheme != "https" && parsedAuthorizationURL.Scheme != "http") || parsedAuthorizationURL.Host == "" || parsedAuthorizationURL.User != nil || parsedAuthorizationURL.Fragment != "" {
		return Pending{}, errors.New("OIDC authorization URL is unsafe")
	}
	now := broker.now()
	pending := pendingState{
		Pending:  Pending{ID: id, State: state, TenantID: tenantID, URL: authorizationURL},
		nonce:    nonce,
		verifier: verifier,
		created:  now,
		expires:  now.Add(broker.pendingTTL),
	}
	broker.mu.Lock()
	broker.cleanupPendingLocked(now)
	if _, exists := broker.pending[id]; exists {
		broker.mu.Unlock()
		return Pending{}, errors.New("OIDC pending identifier collision")
	}
	broker.evictOldestPendingLocked()
	broker.pending[id] = pending
	broker.mu.Unlock()
	return pending.Pending, nil
}

// Complete consumes authorization state and creates a fresh token-bound client/session.
func (broker *Broker) Complete(ctx context.Context, pendingID, state, code string) (SessionResult, error) {
	now := broker.now()
	broker.mu.Lock()
	pending, exists := broker.pending[pendingID]
	delete(broker.pending, pendingID)
	broker.mu.Unlock()
	if !exists || !now.Before(pending.expires) || !constantTimeEqual(pending.State, state) || code == "" {
		return SessionResult{}, ErrInvalidState
	}
	tenant := broker.tenants[pending.TenantID]
	identity, err := tenant.Identity.Exchange(ctx, ExchangeRequest{Code: code, Verifier: pending.verifier, ExpectedNonce: pending.nonce})
	if err != nil {
		return SessionResult{}, fmt.Errorf("exchange OIDC code: %w", err)
	}
	if identity.Subject == "" || identity.AccessToken == "" || !boundedIdentity(identity) || !constantTimeEqual(identity.Nonce, pending.nonce) {
		return SessionResult{}, errors.New("OIDC identity is incomplete or nonce is invalid")
	}
	client, err := broker.clientFactory(ctx, tenant.ProviderBaseURL, identity.AccessToken)
	if err != nil {
		return SessionResult{}, fmt.Errorf("create tenant provider client: %w", err)
	}
	sessionID, err := broker.random(32)
	if err != nil {
		client.Close()
		return SessionResult{}, fmt.Errorf("create session ID: %w", err)
	}
	csrf, err := broker.random(32)
	if err != nil {
		client.Close()
		return SessionResult{}, fmt.Errorf("create CSRF token: %w", err)
	}
	roles := normalizedRoles(identity.Roles)
	session := &sessionState{
		id:           sessionID,
		csrf:         csrf,
		tenantID:     pending.TenantID,
		subject:      identity.Subject,
		displayName:  identity.DisplayName,
		roles:        roles,
		accessToken:  identity.AccessToken,
		refreshToken: identity.RefreshToken,
		tokenExpiry:  identity.Expiry,
		created:      now,
		expires:      now.Add(broker.sessionTTL),
		active:       true,
		client:       client,
		cache:        make(map[string]any),
		optimistic:   make(map[string]any),
	}
	broker.mu.Lock()
	expired := broker.cleanupSessionsLocked(now)
	if _, exists := broker.sessions[sessionID]; exists {
		broker.mu.Unlock()
		for _, stale := range expired {
			deactivate(stale)
		}
		deactivate(session)
		return SessionResult{}, errors.New("Studio session identifier collision")
	}
	evicted := broker.evictOldestSessionLocked()
	broker.sessions[sessionID] = session
	broker.mu.Unlock()
	for _, stale := range expired {
		deactivate(stale)
	}
	if evicted != nil {
		deactivate(evicted)
	}
	return SessionResult{ID: sessionID, CSRF: csrf}, nil
}

func normalizedRoles(roles []string) []string {
	unique := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if role != "" {
			unique[role] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for role := range unique {
		result = append(result, role)
	}
	sort.Strings(result)
	return result
}

func boundedIdentity(identity Identity) bool {
	if len(identity.Subject) > 1024 || len(identity.DisplayName) > 1024 || len(identity.Roles) > 256 || len(identity.AccessToken) > 16*1024 || len(identity.RefreshToken) > 16*1024 {
		return false
	}
	for _, role := range identity.Roles {
		if len(role) > 128 {
			return false
		}
	}
	return true
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func randomValue(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
