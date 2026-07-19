# DEC-008: Contract-complete role operations with scored evidence

Status: accepted for implementation on 2026-07-19

## Decision and scoring

Scores use contract fidelity (25), authorization/tenant security (25), destructive/concurrency safety (20), accessible UX (15), and evidence/operability (15). Anything below 90 or missing a critical control is rejected.

| Option | Score | Decision |
|---|---:|---|
| Contract 1.2 create plus server-held deletion plan/confirmed apply | 98 | Selected. It preserves Provider authority, role separation, reviewed mutation basis, conflicts, idempotency, and receipts. |
| Direct DELETE from the entity page | 61 | Rejected. No dependency review or server-held confirmation basis. |
| Browser-held deletion plan and entity version | 55 | Rejected. Hidden fields could change the reviewed mutation basis. |
| Generic CRUD for audit/usage/discrepancies | 18 | Rejected. Those domains are intentionally observational and have no mutation contract. |

## Consequences

- Provider contract 1.2.0 is the sole operation authority and remains additive to 1.1.0.
- Create and update share the PUT contract but have distinct Studio routes, presentation, and idempotency scopes.
- Deletion requires `studio.metadata.apply`; `studio.metadata.write` alone cannot preview or apply it.
- The local Provider is extended for complete offline UI verification and records every mutation in audit output.
- A machine scorecard must cover exactly the pinned OpenAPI operations and is a required test artifact, not prose-only status.
- Existing v1.0.0 release evidence stays historical; v1.1.0 evidence is a new candidate record and cannot claim publication before remote read-back.
