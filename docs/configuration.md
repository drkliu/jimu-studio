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

## Deployment topology

The v1.0.0 support boundary is one Studio process behind production TLS termination. Session and operator state are volatile process memory. A restart intentionally logs users out, and multiple instances require session affinity without shared-session failover. TLS termination, process supervision, secret injection, provider backups, and provider HTTP handler bindings remain deployment-owner responsibilities.

Chrome on the protected Ubuntu CI runner is the release-certified browser. Other Chromium builds may work, but Firefox and Safari are not certified for v1.0.0.

## Docker-free local PostgreSQL stack

The native local Provider and Dex use PostgreSQL; neither has an in-memory persistence fallback. PostgreSQL must be reachable at `127.0.0.1:5432`. Run `setup-postgres.bat` once with `JIMU_POSTGRES_ADMIN_PASSWORD` and a URL-safe `JIMU_STUDIO_POSTGRES_PASSWORD` set in the environment. The setup creates two least-scope login roles and owned databases:

| Process | Role | Database | Persistent data |
|---|---|---|---|
| Local Jimu Provider | `jimu_studio` | `jimu_studio_local` | resources, versions, idempotent results, and audit history |
| Dex OIDC | `jimu_dex` | `jimu_dex_local` | OIDC clients, authorization state, refresh tokens, and password records |

Then restart `run-provider.bat` and `run-oidc.bat`. Both scripts load `JIMU_STUDIO_POSTGRES_PASSWORD` from the current process or the Windows user environment. The Provider health response must include `"storage":"postgresql"`. Local transport is loopback and uses `sslmode=disable`; production PostgreSQL deployments must use authenticated TLS and managed secret injection.
