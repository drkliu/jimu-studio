# DEC-004: S3 server-held migration plan and apply

Status: accepted on 2026-07-16

## Reconciliation and scoring

Studio main `56b701e`, provider main `610cfc13c69fc007cc835613d5f6302b42d94fa0`, open PRs, protected checks, cursor, and Studio contract 1.0.0 were reconciled before design. The pinned manifest verified before implementation. Provider graph review confirms that planning previews inside a rolled-back transaction; the external contract remains the only UI authority.

Scores use contract fidelity (30), dangerous-operation safety (30), tenant isolation (15), accessible recovery (15), and operational simplicity (10). Work below 70 is rejected.

| Option | Score | Decision |
|---|---:|---|
| Server-held planned entity snapshot plus opaque volatile draft reference | 96 | Selected. The browser cannot change the planned payload, confirmation, provider key, or tenant client. |
| Round-trip planned entities through hidden browser fields | 58 | Rejected. The apply payload would no longer be the reviewed server snapshot. |
| Apply by sending `plan_code` to the provider | 35 | Rejected. Studio 1.0.0 defines no such request field. |
| Browser-to-provider plan/apply client | 12 | Rejected. It violates the bearer-token and trusted-boundary requirements. |

## Design

- A write-authorized operator selects one entity from the bounded metadata workspace. Studio refetches that exact contract entity and sends it to `studio.metadata.plan` with a stable, server-generated idempotency key.
- Plan results are strictly decoded and bounded. Studio renders only contract fields (`plan_code`, `entity_code`, `risk`, `summary`, and `requires_confirmation`) as text; it never accepts or displays SQL.
- The exact planned entities and plan rows live in the session's bounded volatile state under a random opaque draft reference. The browser receives only that reference and CSRF proof. Tenant switch, logout, expiry, and successful apply destroy the state.
- Apply is impossible without a stored plan in the same active tenant session. Studio obtains the stored entity versions, requires the exact `APPLY_MIGRATION` text, and sends the unchanged planned entities with a stable server-held apply idempotency key.
- The v1 apply contract has no top-level `expected_version`; Studio therefore preserves each planned entity's contract `version` as the concurrency basis and never invents an unsupported field. A provider 409 stops the operation, refetches current metadata, and requires a new plan.
- `studio.metadata.write` gates planning and `studio.metadata.apply` gates apply presentation and handling. Provider 401/403 remains authoritative.
- Validation, permission denial, confirmation mismatch, conflict, replay, and safe error handling are proven in unit and Go browser tests.

## RED -> GREEN plan

1. RED provider tests for exact plan/apply JSON, fixed `APPLY_MIGRATION`, strict bounded plan rows, and safe errors.
2. RED session tests for typed volatile migration drafts, capacity limits, stable apply keys, and disposal on tenant switch.
3. RED handler tests proving plan-before-apply, role and CSRF gates, exact typed confirmation, unchanged entity snapshots, stable replay keys, conflict replan, and no SQL rendering.
4. GREEN implement the typed adapter, volatile draft operations, handlers, templates, and metadata entry point; refactor shared provider/error helpers.
5. Verify Studio 1.0.0 again, then formatting, vet, race/full tests, build, module/vulnerability audit, and uncached Go browser E2E/accessibility.
