# Studio configuration

Set `STUDIO_CONFIG` to a JSON file containing only non-secret tenant registration data. Every OIDC client secret is loaded indirectly from the named environment variable and is never rendered, logged, or persisted by Studio.

```json
{
  "development": false,
  "tenants": [
    {
      "id": "alpha",
      "name": "Alpha",
      "provider_base_url": "https://provider.alpha.example",
      "issuer": "https://identity.alpha.example",
      "client_id": "jimu-studio",
      "client_secret_env": "OIDC_ALPHA_CLIENT_SECRET",
      "redirect_url": "https://studio.example/auth/callback",
      "role_claim": "roles"
    }
  ]
}
```

Production endpoints must use HTTPS. Studio expects TLS termination at the deployment boundary and always marks its production cookies `Secure`, `HttpOnly`, and host-only. `development: true` permits HTTP only for loopback identity/provider endpoints and requires Studio itself to listen on explicit loopback; it must never be used in a shared environment.

`STUDIO_ADDRESS` defaults to `127.0.0.1:8080`. Bearer and refresh tokens, OIDC state, nonce, PKCE verifier, CSRF proof, provider clients, cached data, and optimistic edits are bounded and held only in volatile server memory.

Tenant switching invalidates the old session and token-bound client before starting a new authorization flow. The continuation page is intentional: it allows external OIDC navigation while preserving the restrictive `form-action 'self'` content-security policy.
