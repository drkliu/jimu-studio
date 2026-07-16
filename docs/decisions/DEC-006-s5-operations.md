# DEC-006: S5 contract-grounded operations console

Status: accepted on 2026-07-16

## Reconciliation and decision

Studio main `7b92fe5`, provider main `0a9a8c662f2b8290c3f911c97b0a80eb55d560d0`, the merged S4 cursor, and the provider-owned generated bundle were reconciled before implementation. Provider contract 1.1.0 resolves BLK-003 by adding bounded `studio.workflow.tasks.list` discovery with authoritative task versions, timing, redacted error codes, and lease/recovery state. Studio copies the bundle byte-for-byte and pins provider commit `0a9a8c662f2b`.

The selected design is a server-rendered Go BFF with bounded cursor pages for workflow runs/tasks, quota plans/usage/discrepancies, and audit. Bearer tokens, provider clients, idempotency keys, and retry state remain in the volatile tenant session. Provider authorization and optimistic conflicts remain authoritative.

## Safety invariants

- Run cancellation and task retry use only displayed provider IDs and versions, exact `CANCEL_RUN` / `RETRY_TASK` confirmations, CSRF proof, and server-generated stable idempotency keys.
- Task input, result, worker identity, lease owner/fencing data, raw errors, and tenant identifiers never enter the contract or UI.
- Quota publication requires `PUBLISH_QUOTA_PLAN`, an explicit expected version, effective time, bounded limits object, and a stable server-generated key. Revisions are presented as immutable effective-time records.
- Usage and discrepancy statistics are labeled observational and never presented as synchronous enforcement authority.
- Audit pages render only bounded provider-redacted identity/operation fields and redaction paths; detail payloads are neither rendered nor exported.
- Every list is capped at 50 items per provider request, cursors/search are bounded, tenant switch destroys the client and all mutation keys, and 401/403/409 responses are surfaced without bypass or silent retry.

## Verification plan

1. Verify the copied 1.1.0 manifest and exact provider generation.
2. Prove adapter paths, query bounds, strict response decoding, mutation bodies, confirmations, versions, and stable keys.
3. Prove route role/CSRF gates, accessible semantic tables, redaction, conflict recovery, and tenant isolation.
4. Run formatting, unit/race, vet, build, vulnerability, dependency, and authenticated browser gates before advancing the cursor to S6.
