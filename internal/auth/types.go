// Package auth implements volatile OIDC state, sessions, CSRF protection, and
// tenant-scoped provider-client lifecycles for the Studio BFF.
package auth

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	ErrInvalidState   = errors.New("invalid or expired OIDC state")
	ErrInvalidCSRF    = errors.New("invalid CSRF token")
	ErrUnknownTenant  = errors.New("unknown Studio tenant")
	ErrUnknownSession = errors.New("unknown Studio session")
)

// AuthorizationRequest contains standard OIDC and PKCE authorization values.
type AuthorizationRequest struct {
	State         string
	Nonce         string
	CodeChallenge string
}

// ExchangeRequest contains the single-use authorization response proof.
type ExchangeRequest struct {
	Code          string
	Verifier      string
	ExpectedNonce string
}

// Token is server-only OAuth token state. It must never be rendered or logged.
type Token struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// Identity is the verified OIDC identity plus its server-only provider token.
type Identity struct {
	Subject      string
	DisplayName  string
	Roles        []string
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	Nonce        string
}

// IdentityProvider is implemented by a tenant-specific standard OIDC registration.
type IdentityProvider interface {
	AuthorizationURL(AuthorizationRequest) string
	Exchange(context.Context, ExchangeRequest) (Identity, error)
	Refresh(context.Context, string) (Token, error)
}

// TenantClient is a token-bound provider client whose close operation cancels requests.
type TenantClient interface {
	Close()
}

// Tenant defines one explicit OIDC registration and provider origin.
type Tenant struct {
	ID              string
	Name            string
	ProviderBaseURL string
	Identity        IdentityProvider
}

// TenantView is safe to render in the browser.
type TenantView struct {
	ID   string
	Name string
}

// ClientFactory creates a fresh provider client from a newly acquired token.
type ClientFactory func(context.Context, string, string) (TenantClient, error)

// Config controls bounded volatile authentication state.
type Config struct {
	Tenants       []Tenant
	ClientFactory ClientFactory
	PendingTTL    time.Duration
	SessionTTL    time.Duration
	RefreshSkew   time.Duration
	MaxPending    int
	MaxSessions   int
	Now           func() time.Time
	Random        func(int) (string, error)
}

// Pending is browser-safe metadata for an authorization redirect.
type Pending struct {
	ID       string
	State    string
	TenantID string
	URL      string
}

type pendingState struct {
	Pending
	nonce    string
	verifier string
	created  time.Time
	expires  time.Time
}

// SessionResult is returned once, so the HTTP boundary can set the opaque cookie.
type SessionResult struct {
	ID   string
	CSRF string
}

// SessionView contains only presentation-safe session information.
type SessionView struct {
	TenantID    string
	TenantName  string
	Subject     string
	DisplayName string
	Roles       []string
	CSRF        string
}

// HasRole supports presentation-only navigation filtering.
func (view SessionView) HasRole(role string) bool {
	index := sort.SearchStrings(view.Roles, role)
	return index < len(view.Roles) && view.Roles[index] == role
}

type sessionState struct {
	mu           sync.Mutex
	id           string
	csrf         string
	tenantID     string
	subject      string
	displayName  string
	roles        []string
	accessToken  string
	refreshToken string
	tokenExpiry  time.Time
	created      time.Time
	expires      time.Time
	active       bool
	client       TenantClient
	cache        map[string]any
	optimistic   map[string]any
}

// Broker owns all volatile authentication state.
type Broker struct {
	mu            sync.RWMutex
	tenants       map[string]Tenant
	tenantViews   []TenantView
	pending       map[string]pendingState
	sessions      map[string]*sessionState
	clientFactory ClientFactory
	pendingTTL    time.Duration
	sessionTTL    time.Duration
	refreshSkew   time.Duration
	maxPending    int
	maxSessions   int
	now           func() time.Time
	random        func(int) (string, error)
}
