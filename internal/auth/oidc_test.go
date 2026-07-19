package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOIDCProviderUsesPKCEAndVerifiesSignedIdentity(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	identityToken := func(nonce string) string {
		t.Helper()
		return signedTestToken(t, key, map[string]any{
			"iss": issuer, "aud": "studio-client", "sub": "operator", "name": "Operator",
			"nonce": nonce, "groups": []string{"studio.metadata.read"},
			"iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			writeJSON(t, response, map[string]any{
				"issuer": issuer, "authorization_endpoint": issuer + "/authorize",
				"token_endpoint": issuer + "/token", "jwks_uri": issuer + "/jwks",
				"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/jwks":
			writeJSON(t, response, map[string]any{"keys": []map[string]any{{
				"kty": "RSA", "kid": "test-key", "use": "sig", "alg": "RS256",
				"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()), "e": exponent(key.PublicKey.E),
			}}})
		case "/token":
			if err := request.ParseForm(); err != nil {
				http.Error(response, "bad form", http.StatusBadRequest)
				return
			}
			if request.Form.Get("grant_type") == "authorization_code" && request.Form.Get("code_verifier") == "verifier-value" {
				writeJSON(t, response, map[string]any{
					"access_token": fmt.Sprintf("opaque-%d", time.Now().UnixNano()), "refresh_token": fmt.Sprintf("refresh-%d", time.Now().UnixNano()),
					"token_type": "Bearer", "expires_in": 3600, "id_token": identityToken("nonce-value"),
				})
				return
			}
			http.Error(response, "unsupported grant", http.StatusBadRequest)
		default:
			http.NotFound(response, request)
		}
	}))
	issuer = server.URL
	t.Cleanup(server.Close)

	provider, err := NewOIDCProvider(context.Background(), OIDCConfig{
		Issuer: issuer, ClientID: "studio-client", ClientSecret: t.Name(), RedirectURL: issuer + "/callback", RoleClaim: "groups",
	})
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := url.Parse(provider.AuthorizationURL(AuthorizationRequest{State: "state-value", Nonce: "nonce-value", CodeChallenge: "challenge-value"}))
	if err != nil {
		t.Fatal(err)
	}
	if authorization.Query().Get("state") != "state-value" || authorization.Query().Get("nonce") != "nonce-value" || authorization.Query().Get("code_challenge_method") != "S256" || authorization.Query().Get("code_challenge") != "challenge-value" {
		t.Fatalf("authorization URL omitted OIDC/PKCE proof: %s", authorization.String())
	}
	if !strings.Contains(" "+authorization.Query().Get("scope")+" ", " groups ") {
		t.Fatalf("authorization URL omitted configured groups scope: %s", authorization.String())
	}
	identity, err := provider.Exchange(context.Background(), ExchangeRequest{Code: "single-use-code", Verifier: "verifier-value", ExpectedNonce: "nonce-value"})
	if err != nil {
		t.Fatal(err)
	}
	if identity.Subject != "operator" || identity.DisplayName != "Operator" || len(identity.Roles) != 1 || identity.Nonce != "nonce-value" || identity.AccessToken == "" || identity.RefreshToken == "" {
		t.Fatalf("verified identity is incomplete: %#v", identity)
	}
}

func signedTestToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": "test-key", "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func exponent(value int) string {
	return base64.RawURLEncoding.EncodeToString(big.NewInt(int64(value)).Bytes())
}

func writeJSON(t *testing.T, response http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(response).Encode(value); err != nil && !strings.Contains(err.Error(), "closed") {
		t.Errorf("write fake OIDC response: %v", err)
	}
}
