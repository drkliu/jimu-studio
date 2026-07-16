# DEC-002: S1 authenticated Go shell and tenant isolation

Status: accepted on 2026-07-16

## Reconciliation and dependency-ready scoring

S0 and its cursor are merged; protected post-merge CI and CodeQL are green. Five generated GitHub Actions upgrades were reconciled, verified, and merged before S1. The provider remains fixed at `610cfc13c69fc007cc835613d5f6302b42d94fa0`; Studio contract `1.0.0` is unchanged.

Scores use token/tenant security (35), contract fidelity (25), accessibility/testability (20), and operational risk (20). Work below 70 is rejected.

| Option | Score | Decision |
|---|---:|---|
| Go BFF with tenant-specific standard OIDC registrations, volatile sessions, and server-side provider clients | 94 | Selected. No bearer token or trusted identity header enters browser code. |
| Go WebAssembly client holding bearer tokens in memory | 62 | Rejected. Browser requests and client lifetime make tenant teardown and header safety weaker. |
| One OIDC registration plus an invented `tenant` authorization parameter | 45 | Rejected. The provider handoff defines no such identity-provider contract. |
| Persistent encrypted browser token storage | 20 | Rejected by the explicit non-persistence requirement. |

## Design

- Each configured tenant has its own standard OIDC issuer/client/redirect registration and Jimu provider base URL. Tenant switching starts a new Authorization Code + PKCE flow against the target registration; it does not invent a tenant query contract.
- OIDC state, nonce, PKCE verifier, access/refresh tokens, roles, CSRF values, tenant caches, and provider clients live only in bounded server memory. Logs, URLs, fixtures, source, and browser storage never receive bearer tokens.
- The browser holds only opaque `HttpOnly`, `Secure`, `SameSite` cookies. Authentication callback state is single-use and time-bounded.
- Switching first invalidates the old session, cancels its requests, closes its provider client, and clears its caches/optimistic state. Completion creates a new session and fresh provider client from the newly issued target-tenant token.
- Provider HTTP redirects are rejected so authorization cannot leak cross-origin. Requests add `Authorization` and `Accept` only; trusted tenant/user headers are forbidden.
- Role-aware navigation is presentation only. Every protected operation still relies on provider 401/403 authorization.
- Refresh tokens remain server-side. Refresh replaces the access token and provider client atomically; refresh failure invalidates the session.
- `coreos/go-oidc/v3` and `golang.org/x/oauth2` implement standards verification and token exchange. Both are established dependencies already used or permitted by the Jimu platform stack.

## RED to GREEN plan

1. RED: provider-client tests reject unsafe origins/tokens, prevent redirects, emit no trusted headers, and prove cancellation.
2. RED: broker tests prove single-use state/nonce/PKCE handling, bounded volatile sessions, CSRF, refresh replacement, and tenant-switch disposal/isolation.
3. GREEN: implement OIDC adapter, broker/session store, provider lifecycle, secure cookies, authenticated shell, role-aware navigation, logout, and switch handlers.
4. Refactor: centralize constant-time comparisons, bounded cleanup, error mapping, and semantic templates.
5. Verify the pinned provider artifact first, then unit/race/vet/vulnerability/license checks and two-tenant Go browser E2E with no cache, edit, token, header, or request reuse.
