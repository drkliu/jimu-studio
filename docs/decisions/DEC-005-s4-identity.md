# DEC-005: S4 bounded users and roles administration

Status: accepted on 2026-07-16

## Reconciliation and scoring

Studio main `c5b4d27`, provider main `610cfc13c69fc007cc835613d5f6302b42d94fa0`, open PRs, cursor, and Studio contract 1.0.0 were reconciled and the pinned manifest verified before implementation. Provider graph review confirms system-role immutability and last-active-admin protection remain provider-authoritative.

Scores use contract fidelity (30), identity safety (30), concurrency/recovery (20), accessibility (10), and operational simplicity (10). Work below 70 is rejected.

| Option | Score | Decision |
|---|---:|---|
| Server-rendered Go BFF with bounded dual cursor lists and operation-specific forms | 95 | Selected. Tokens and trusted identity stay server-side; versions and stable keys are explicit. |
| Browser-managed identity SPA or WebAssembly client | 43 | Rejected. It expands the bearer/state boundary and adds no contract capability. |
| Generic JSON identity editor | 28 | Rejected. It could request credentials or bypass exact operation semantics. |

## Design and RED -> GREEN plan

- `GET /identity` loads at most 50 users and 50 roles with independent opaque cursors/search. Semantic tables remain usable without visual layout.
- The UI creates users and roles, changes user status, assigns/revokes roles, and updates only non-system roles. It never renders or requests passwords, tokens, secrets, or credential material.
- Every mutation is same-site/CSRF/`identity.admin` gated, uses a server-generated stable idempotency key derived from a hash of the mutation basis, and sends the displayed expected version where the contract requires it.
- Status changes require the exact `DISABLE_USER` confirmation defined by Studio 1.0.0. System roles are visibly immutable and rejected server-side before provider mutation. Provider last-admin and authorization rejection remain authoritative.
- A 409 reloads bounded current state and explicitly asks the operator to reconcile the refreshed version. Tenant switch destroys all mutation keys with the session/client.
- RED provider tests cover every exact method/path/body and strict decoding. RED handler/browser tests cover bounded cursors, no credential fields, permission denial, exact confirmation, system-role rejection, last-admin display, conflict refresh, stable retry keys, keyboard labels, and tenant boundaries. GREEN then implements adapter, handlers/templates, and full contract/race/browser/security gates.
