package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var safeClaimName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

// OIDCConfig defines one standards-based tenant registration.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	RoleClaim    string
}

// OIDCProvider verifies ID tokens and exchanges/refreshes OAuth tokens server-side.
type OIDCProvider struct {
	oauth     oauth2.Config
	verifier  *oidc.IDTokenVerifier
	roleClaim string
}

// NewOIDCProvider performs discovery and pins the issuer/client verifier.
func NewOIDCProvider(ctx context.Context, config OIDCConfig) (*OIDCProvider, error) {
	if config.Issuer == "" || config.ClientID == "" || config.RedirectURL == "" {
		return nil, errors.New("OIDC issuer, client ID, and redirect URL are required")
	}
	if config.RoleClaim == "" {
		config.RoleClaim = "roles"
	}
	if !safeClaimName.MatchString(config.RoleClaim) {
		return nil, errors.New("OIDC role claim name is invalid")
	}
	scopes := []string{oidc.ScopeOpenID, "profile", "email", "offline_access"}
	if config.RoleClaim == "groups" {
		scopes = append(scopes, "groups")
	}
	discovery, err := oidc.NewProvider(ctx, config.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	return &OIDCProvider{
		oauth: oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			Endpoint:     discovery.Endpoint(),
			RedirectURL:  config.RedirectURL,
			Scopes:       scopes,
		},
		verifier:  discovery.Verifier(&oidc.Config{ClientID: config.ClientID}),
		roleClaim: config.RoleClaim,
	}, nil
}

// AuthorizationURL creates a standard authorization-code request with nonce and S256 PKCE.
func (provider *OIDCProvider) AuthorizationURL(request AuthorizationRequest) string {
	return provider.oauth.AuthCodeURL(
		request.State,
		oidc.Nonce(request.Nonce),
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challenge", request.CodeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

// Exchange validates the signed ID token and returns only server-held OAuth state.
func (provider *OIDCProvider) Exchange(ctx context.Context, request ExchangeRequest) (Identity, error) {
	if request.Code == "" || request.Verifier == "" || request.ExpectedNonce == "" {
		return Identity{}, errors.New("OIDC code, PKCE verifier, and nonce are required")
	}
	token, err := provider.oauth.Exchange(ctx, request.Code, oauth2.SetAuthURLParam("code_verifier", request.Verifier))
	if err != nil {
		return Identity{}, err
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Identity{}, errors.New("OIDC token response omitted id_token")
	}
	idToken, err := provider.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Identity{}, fmt.Errorf("verify OIDC ID token: %w", err)
	}
	var claims struct {
		Subject           string `json:"sub"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
		Nonce             string `json:"nonce"`
	}
	if err = idToken.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("decode OIDC identity claims: %w", err)
	}
	var rawClaims map[string]json.RawMessage
	if err = idToken.Claims(&rawClaims); err != nil {
		return Identity{}, fmt.Errorf("decode OIDC role claims: %w", err)
	}
	var roles []string
	if encodedRoles := rawClaims[provider.roleClaim]; len(encodedRoles) > 0 {
		if err = json.Unmarshal(encodedRoles, &roles); err != nil {
			return Identity{}, fmt.Errorf("decode OIDC %s claim: %w", provider.roleClaim, err)
		}
	}
	displayName := firstNonEmpty(claims.Name, claims.PreferredUsername, claims.Email, claims.Subject)
	return Identity{
		Subject:      claims.Subject,
		DisplayName:  displayName,
		Roles:        roles,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
		Nonce:        claims.Nonce,
	}, nil
}

// Refresh obtains a fresh access token without exposing refresh state to the browser.
func (provider *OIDCProvider) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return Token{}, errors.New("OIDC refresh token is required")
	}
	seed := &oauth2.Token{RefreshToken: refreshToken, Expiry: time.Unix(0, 0)}
	refreshed, err := provider.oauth.TokenSource(ctx, seed).Token()
	if err != nil {
		return Token{}, fmt.Errorf("refresh OIDC token: %w", err)
	}
	return Token{AccessToken: refreshed.AccessToken, RefreshToken: refreshed.RefreshToken, Expiry: refreshed.Expiry}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
