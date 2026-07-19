# Full role-governed Studio operations

Status: implementation candidate on 2026-07-19

## Objective and boundary

Jimu Studio must expose every operation in Provider contract 1.2.0 through the correct role boundary, while keeping tenant identity, bearer tokens, mutation keys, reviewed snapshots, and provider authorization server-side. “Full” means the complete domain operation set, not artificial CRUD: audit, usage, and discrepancies remain read-only because the contract defines them as observations.

Provider PR #64 merged contract 1.2.0 at `bf130c33ba3d47bb7239d238b241b853df72f24e`. Entity creation uses the existing versioned PUT with `expected_version=0`. Deletion adds a provider-authoritative dependency preview and a separately confirmed DELETE receipt.

## Role-operation model

| Area | Read | Standard mutation | Dangerous mutation |
|---|---|---|---|
| Metadata | `studio.metadata.read` | `studio.metadata.write` creates/updates and plans migrations | `studio.metadata.apply` applies migrations and plans/applies deletion |
| Identity | `identity.admin` | `identity.admin` creates/updates users and roles, assigns/revokes roles | `identity.admin` disables users with exact confirmation |
| Workflow | `studio.workflow.read` | — | `studio.workflow.operate` cancels runs and retries tasks |
| Quota | `studio.quota.read` | — | `studio.quota.admin` publishes immutable plan revisions |
| Audit | `studio.audit.read` | None by design | None by design |

Every browser mutation requires same-site proof, an authenticated same-tenant session, the operation role before provider/draft access, bounded form parsing, CSRF proof, and a server-generated idempotency key. Versioned operations send the provider version. Dangerous operations require the contract’s exact typed confirmation.

## Metadata create and delete

Create has a dedicated `/metadata/new` editor and `/metadata/create` handler. Entity code is editable only for create, the expected version is fixed to zero, duplicate codes are provider conflicts, and the accessible dynamic field editor reindexes at most 200 fields before submit.

Delete is a two-step workflow:

1. Studio refetches the entity, requests the Provider deletion plan for that exact version, and stores the plan plus its mutation basis in bounded volatile session state.
2. The browser receives only an opaque draft reference, CSRF token, impact text, and dependencies.
3. Apply is unavailable when `deletable=false` and requires exact `DELETE_ENTITY` when allowed.
4. Studio deletes only the stored code/version with a stable server-held key. Conflict destroys the stale draft and requires replanning; success destroys the draft and renders the provider receipt.

## Scoring and production gate

Each of the 24 contract operations is scored out of 100:

- contract fidelity: 25;
- authorization and tenant security: 25;
- concurrency, idempotency, destructive-operation, or verified read-only safety: 20;
- accessible browser UX: 15;
- automated evidence and operability: 15.

The production threshold is 90. A missing operation, method/path/role mismatch, missing evidence, or false critical control rejects the score regardless of total. A category may receive full credit when its non-applicability is itself verified—for example, the public contract document has no tenant data, and audit is contractually read-only.

The machine record is `docs/releases/v1.1.0-operation-scorecard.json` and is checked against the pinned OpenAPI document by `internal/scorecard`. Scores remain candidate evidence until protected CI and browser gates are recorded.

## Local production-parity stack

`run-oidc.bat` starts native Dex on port 5556. `run-provider.bat` starts the bounded in-memory Provider on port 8081. `run-local.bat` starts Studio on port 8080 with the ignored local secret configuration. The local Provider implements every mutation family with concurrency, typed confirmation, idempotency, and audit append behavior; it is a reference/test service, not a persistent production data store.

## Release boundary

Production support remains one Studio process behind TLS termination with deployment-owner secret injection, supervision, and Provider backups. Volatile Studio state means restart logs users out and invalidates drafts. Release publication requires protected CI, CodeQL, dependency review, vulnerability checks, and browser accessibility evidence; no tag or GitHub release is created without an explicit release-publication decision.
