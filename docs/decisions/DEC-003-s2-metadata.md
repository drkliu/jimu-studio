# DEC-003: S2 contract-faithful metadata workspace

Status: accepted on 2026-07-16

## Reconciliation and scoring

Studio main `96a05be`, provider main `610cfc13c69fc007cc835613d5f6302b42d94fa0`, protected CI, CodeQL, open PRs, cursor, and Studio contract 1.0.0 were reconciled before design. The pinned manifest verified before implementation. BLK-002 was resolved by explicitly limiting S2 to entity/field visualization and guarded entity editing.

Scores use contract fidelity (30), tenant/security safety (25), accessible UX (20), concurrency/recovery (15), and operational simplicity (10). Work below 70 is rejected.

| Option | Score | Decision |
|---|---:|---|
| Server-rendered Go BFF with typed provider adapter and volatile per-session drafts | 94 | Selected. Keeps bearer tokens and provider traffic server-side and makes tenant teardown authoritative. |
| Go WebAssembly metadata client | 63 | Rejected. It would move provider authorization and optimistic state into the browser. |
| Generate or hand-maintain a browser TypeScript client | 41 | Rejected. It conflicts with the Go-only toolchain and exposes bearer traffic to browser code. |
| Infer entity relations from field strings | 32 | Rejected by BLK-002 and contract authority. |

## Design

- `GET /metadata` requests at most 50 entities using the contract cursor/search query and renders both a compact entity/field map and a semantic table. Only the opaque `next_cursor` is exposed; cursors and search are bounded before provider use.
- `GET /metadata/edit` performs a bounded exact-code search (maximum 200) because 1.0.0 has no entity-detail read operation. Absence is reported; the UI never scans unbounded pages.
- The edit form is generated from the provider entity shape. Entity/field values are bounded and validated without adding undocumented data-type semantics.
- Every mutation uses the displayed provider version as `expected_version`. A server-generated idempotency key is stable for the session/entity/version draft and is never accepted from the browser.
- Draft keys and optimistic state live in the session's bounded volatile map, so tenant switching or logout destroys them with the provider client and cached state.
- A 409 response triggers a fresh bounded fetch and an explicit reconciliation page showing submitted and current versions; Studio never silently overwrites or auto-retries with a newer version.
- 400 validation, 401/403 authorization, 404, 409 conflict, and provider failures are rendered as safe public messages. Provider request IDs may be shown; response bodies, bearer tokens, and raw SQL are never logged or rendered.
- A small dependency-free browser guard marks changed forms and warns before accidental navigation. It requires no Node.js toolchain and handles no token/provider data.

## RED → GREEN plan

1. RED typed-provider tests for bounded cursors, exact JSON shapes, expected versions, stable idempotency values, strict decoding, redirects, and safe API errors.
2. RED session tests for client access, stable bounded draft keys, successful clearing, and disposal on tenant switch.
3. RED server tests for accessible list/table output, guarded form parsing, provider denial, validation, conflict refetch/reconciliation, and success.
4. GREEN implement the adapter, session operations, handlers, templates, and unsaved-change guard; then refactor shared response/error handling.
5. Verify the pinned manifest again, unit/race/vet/vulnerability/dependency checks, and Go browser E2E for bounded pagination, editing, conflict recovery, keyboard use, and tenant isolation.
