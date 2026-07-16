# BLK-002: Studio 1.0.0 has no metadata relation contract

Status: blocking S2 on 2026-07-16

## Reconciled evidence

- Provider authority is clean `github.com/drkliu/jimu` main at `610cfc13c69fc007cc835613d5f6302b42d94fa0`; there are no open provider PRs.
- Studio main is clean at `5eb4fba` with S1 merged at `10fcffb5cc7d043c13edbec1cd461d8c2433d59c`; there are no open Studio PRs and the cursor activates S2.
- Pinned contract `1.0.0` still verifies. Its OpenAPI digest is `9891172a3d4fa0f75b37a820d481c58e5e68cf12d4bede1cbf07959b0763549f`.
- The accepted external roadmap requires entity/relation visualization with an accessible tabular alternative.
- Studio 1.0.0 defines only `GET /studio/v1/metadata/entities`, `PUT /studio/v1/metadata/entities/{code}`, `POST /studio/v1/metadata/plan`, and `POST /studio/v1/metadata/apply`.
- The entity wire shape contains `code`, `name`, `kind`, `version`, and fields (`code`, `data_type`, `required`, `read_only`). It contains no relation, target entity, cardinality, join, or reference identity.
- `contracts/studio/v1/client.ts`, `fixtures.json`, and `README.md` contain no relation representation or relation operation. The provider generator in `kit/studio.go` registers the same four metadata operations and its entity schema has no relation property.

Therefore a UI cannot distinguish a relation from an ordinary field or obtain its target/cardinality without inventing semantics outside the provider contract. Doing so would violate the contract-authority and drift rules.

## Scored alternatives

Scores use contract fidelity (35), accepted-requirement coverage (30), tenant/security safety (20), and accessibility/testability (15). Work below 70 is rejected.

| Alternative | Score | Disposition |
|---|---:|---|
| Infer relations from `data_type` strings or naming conventions | 32 | Rejected: invents an undocumented provider contract and can render false topology. |
| Render entity-to-field edges and label them as relations | 45 | Rejected: misleading UX and does not satisfy entity-relation semantics. |
| Modify the copied 1.0.0 artifacts in the UI repository | 0 | Rejected: CI manifest verification would and must fail. |
| Implement only entity/field visualization and guarded entity editing while retaining the current relation acceptance criterion | 64 | Rejected under the accepted roadmap: safe partial work cannot close S2 and would obscure the blocker. |
| Explicitly amend S2 to entity/field visualization and guarded editing under 1.0.0 | 82 | Safe only after the requirement owner accepts the reduced scope. |
| Publish a provider-owned versioned contract that models relation reads/edits, then repin Studio | 91 | Safest path if relation visualization remains required; unavailable until provider contract work is authorized and accepted. |

Every presently executable option that preserves all accepted requirements scores below 70.

## Exact decision required

Choose one:

1. Keep relation visualization: authorize a provider-owned, versioned Studio contract extension defining relation identity, source, target, cardinality, list pagination, guarded mutation, expected-version behavior, and migration-plan interaction; then repin the external roadmap to that released contract.
2. Amend the S2 acceptance criterion to entity/field visualization plus guarded entity editing using Studio 1.0.0, explicitly removing relation visualization from this release.

No S2 implementation or contract workaround will proceed until one decision is recorded.
