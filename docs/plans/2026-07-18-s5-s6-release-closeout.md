# Jimu Studio S5-S6 Release Closeout Plan

Spec: `docs/specs/2026-07-18-s5-s6-release-closeout-design.md`

Execution loop: `docs/plans/2026-07-18-s5-s6-release-closeout-loop.md`

Status: complete through release `v1.0.0` and closeout PR #22

## Execution rules

- Work in order. S6 never branches from an unconfirmed or merely local S5 commit.
- Use Go 1.26.5 only; do not introduce a Node.js toolchain.
- Verify live remote state immediately before every merge, tag, or release mutation.
- Preserve the exact provider and Studio contract pins. Generated files are copied or regenerated only through the provider authority and must verify byte-for-byte.
- Keep factual metadata pending until its remote object exists. Never pre-fill a merge commit, run ID, release URL, or release timestamp.
- Do not reuse authorization across mutations: PR #18 merge, S6 merge, tag creation, and release publication are separate decisions.

## Phase 1 - Finish S5 safely

1. [x] Read `git status --short --branch`, PR #18 state/head/checks, protected `main`, open Studio PRs, and existing releases without writing remote state.
2. [x] Reconcile the unchanged green PR #18, correct the stale required check without weakening protection, and perform the authorized administrator squash merge once.
3. [x] Fetch the remote and prove S5 merge `f02a750141b122703e57e6c715adf1c98c0141ee` is on protected `main`.
4. [x] Record successful protected post-merge CI `29639714157` and CodeQL `29639714159`.
5. [x] Reconcile the cursor and provider metadata to contract 1.1.0 through dedicated PR #19.
6. [x] Merge reconciliation at `61c5b7cd711d5516472aa0a3718e711f314caebc`; protected CI `29639992477` and CodeQL `29639992443` succeeded.

Phase exit: S5 code and cursor reconciliation are both on protected `main`; no diagnostic files or generated drift remain.

## Phase 2 - Create the S6 release candidate

1. [x] Create `feature/s6-release-acceptance` from reconciled protected `main` commit `61c5b7cd711d5516472aa0a3718e711f314caebc`.
2. [x] Re-read the spec against live code, CI workflows, provider manifest, branch protection, security advisories, and release state; no release existed.
3. [x] Add the pending-only `v1.0.0` acceptance record with supported deployment/browser matrix, nine limitations, rollback, and provenance.
4. [x] Reconcile `README.md`, `SECURITY.md`, and `docs/configuration.md` without unsupported support, packaging, or SLA claims.
5. [x] Add strict Go validation and RED/GREEN tests for provider/contract identity, digests, gates, candidate claims, support, limitations, and rollback.
6. [x] Keep packaging source/commit-only; no binary or container archive is proposed.

Phase exit: the S6 branch contains a truthful, machine-checkable release candidate record and documentation, with no remote release claimed.

## Phase 3 - Verify the candidate

Run from a clean S6 tree with Go 1.26.5:

1. [x] Verify `contracts/studio/v1` with the pinned provider CLI.
2. [x] Require repository Go sources to have clean `gofmt` output.
3. [x] Run `go test ./... -count=1`.
4. [x] Run `go test -race ./... -count=1`.
5. [x] Run `go vet ./...`.
6. [x] Run `go build ./cmd/studio`.
7. [x] Run module verification; protected dependency review, vulnerability, and CodeQL succeeded.
8. [x] Compile the browser suite locally; local Windows Chrome was unavailable, and protected Linux browser E2E succeeded in CI `29640474590`.
9. [x] Review tenant isolation, OIDC/CSRF, redirects, bounds, roles, idempotency, versions, confirmations, redaction, secrets, and provider internals.
10. [x] Confirm the release diff contains only acceptance/closeout work.

Phase exit: all local gates possible in the environment are green and the candidate is ready for protected CI.

## Phase 4 - Merge S6

1. [x] Commit and push S6; open focused PR #20.
2. [x] Require green contract, Go quality/race, browser E2E, dependency review, vulnerability, and CodeQL on head `964226e063e74315239e816ea8ad07b0e22e4e3f`.
3. [x] Review documentation and provenance against provider manifest, Git, and GitHub runs.
4. [x] Perform the separately approved S6 merge without an administrator override.
5. [x] Merge once at `3ecc4c0a70a5d29da9e929b56dbef97241cbdf5e`; protected CI `29640474590` and CodeQL `29640474587` succeeded.
6. [x] Merge evidence-finalization PR #21 at `2db2c8bcd877174c068f65ed034303c876da7834` before tagging; protected CI `29640625780` and CodeQL `29640625770` succeeded.

Phase exit: the final release candidate commit and complete protected evidence are on `main` and no tag/release yet exists.

## Phase 5 - Publish `v1.0.0`

1. [x] Present exact commit `2db2c8bcd877174c068f65ed034303c876da7834`, evidence, tag, notes, limitations, and rollback; obtain explicit publication authorization.
2. [x] Confirm no `v1.0.0` tag/release existed and protected `main` was the accepted commit.
3. [x] Create and push annotated tag `v1.0.0` at that exact commit.
4. [x] Create the non-draft, non-prerelease GitHub release with the accepted evidence and limitations.
5. [x] Read back tag object `8fdb63b7e5b0b7047a21c60995db85b9b1e1631a`, exact commit, URL, and publication time `2026-07-18T10:24:01Z`.

Phase exit: `v1.0.0` exists, resolves to the accepted protected commit, and exposes complete release evidence.

## Phase 6 - Final cursor closeout

1. [x] Create final branch `chore/v1.0.0-closeout` from the released `main` commit.
2. [x] Update acceptance and cursor with only read-back release facts; set S6 terminal state and clear the active slice/branch.
3. [x] List `v1.0.x` as supported without inventing an SLA.
4. [x] Validate JSON/documentation consistency, rerun contract/unit/race/vet/build/browser/security gates, and open final closeout PR #22.
5. [x] Merge PR #22 once at `de533b4a86d4c44101a8619d41f072b3e8e2b1a5`; protected CI `29640948113` and CodeQL `29640947998` succeeded.
6. [x] Verify the release commit is an ancestor of closeout, S0-S6 records agree, no roadmap PR remains open, and no unresolved blocker exists.

Terminal exit: code, contract, tests, protected CI, release, security support statement, provenance, rollback, limitations, and cursor all agree. Only then may the S0-S6 execution goal be marked complete.

## Failure and rollback branches

- **PR #18 changed or failed:** do not merge; review the delta and rerun the S5 gate.
- **Post-merge S5 failure:** keep S6 inactive, correct S5 through a new reviewed PR, and reconcile the cursor afterward.
- **Candidate gate failure:** fix only the demonstrated defect on S6 and rerun the whole affected gate set.
- **Tag exists unexpectedly:** stop before mutation and reconcile ownership/commit identity.
- **Release validation fails after publication:** remove traffic from the release, deploy the confirmed S5 fallback, invalidate volatile sessions by restart, preserve audit evidence, and correct through a new version. Do not retarget the published `v1.0.0` tag.
- **Provider or contract drift:** stop; a provider upgrade or contract change requires a separate compatibility decision and cannot be folded into release closeout.
