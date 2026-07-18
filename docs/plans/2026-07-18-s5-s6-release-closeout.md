# Jimu Studio S5-S6 Release Closeout Plan

Spec: `docs/specs/2026-07-18-s5-s6-release-closeout-design.md`

Execution loop: `docs/plans/2026-07-18-s5-s6-release-closeout-loop.md`

Status: in progress; phases 1 and S5 reconciliation are complete, S6 candidate implementation is active

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

1. [ ] Verify `contracts/studio/v1` with the pinned provider CLI.
2. [ ] Require `gofmt -l .` to return no files.
3. [ ] Run `go test ./... -count=1`.
4. [ ] Run `go test -race ./... -count=1`.
5. [ ] Run `go vet ./...`.
6. [ ] Run `go build ./cmd/studio`.
7. [ ] Run module/dependency and vulnerability checks used by protected CI.
8. [ ] Compile the browser suite locally. Run it locally when Chrome is available, but treat protected Linux `go test -tags=e2e -timeout=2m ./e2e` as authoritative.
9. [ ] Review tenant isolation, OIDC/CSRF boundaries, provider redirects, bounded inputs/responses, role gates, idempotency, expected-version conflicts, confirmations, redaction, and absence of secrets/raw SQL/workflow internals.
10. [ ] Confirm the release diff contains only acceptance/closeout work or separately explained corrective changes.

Phase exit: all local gates possible in the environment are green and the candidate is ready for protected CI.

## Phase 4 - Merge S6

1. [ ] Commit and push the S6 branch; open one focused PR linking this spec and plan.
2. [ ] Wait for contract, Go quality/race, browser E2E, dependency review, vulnerability, CodeQL, and any branch-protection checks. Record exact run IDs and the candidate commit.
3. [ ] Review the rendered documentation and compare every provenance value with the provider manifest, Git commit, and GitHub runs.
4. [ ] Obtain explicit authorization for any administrator override or merge. PR #18 authorization is not reusable.
5. [ ] Merge once, fetch protected `main`, identify the exact S6 squash commit, and wait for post-merge CI and CodeQL on it.
6. [ ] If the squash commit changes a commit-bound provenance field, create the smallest possible evidence-finalization PR, rerun all required checks, and merge it before tagging. Do not amend or force-push protected `main`.

Phase exit: the final release candidate commit and complete protected evidence are on `main` and no tag/release yet exists.

## Phase 5 - Publish `v1.0.0`

1. [ ] Present the exact candidate commit, evidence, proposed tag, release notes, known limitations, and rollback target; obtain explicit tag/release authorization.
2. [ ] Re-check that no tag or release named `v1.0.0` exists and that protected `main` still points at the accepted commit.
3. [ ] Create an annotated `v1.0.0` tag on that exact commit and push only that tag.
4. [ ] Create a non-draft, non-prerelease GitHub release from the same tag. Include provider/contract pins, acceptance evidence, supported matrix, limitations, rollback, and provenance.
5. [ ] Read the tag and release back from GitHub. Record the immutable URL, commit, publication time, and any release workflow evidence. If read-back differs, stop and report it; do not silently retarget or recreate a published tag.

Phase exit: `v1.0.0` exists, resolves to the accepted protected commit, and exposes complete release evidence.

## Phase 6 - Final cursor closeout

1. [ ] Create a final closeout branch from the released `main` commit.
2. [ ] Update the acceptance record and cursor with only observed release facts. Set `last_merged_slice` to S6, record the exact closeout ancestry/evidence, set `release` to the real tag/URL/commit/time, and clear `active_slice` and `active_branch` using the cursor's documented terminal representation.
3. [ ] Update `SECURITY.md` to list `v1.0.0` as supported without inventing an SLA.
4. [ ] Validate JSON and documentation links, rerun the complete contract/unit/race/vet/build/browser/security matrix, and open the final closeout PR.
5. [ ] Obtain separate merge authorization if required, merge once, and record protected post-merge CI/CodeQL.
6. [ ] Verify the repository is clean, the release remains attached to an ancestor of the closeout commit, all S0-S6 records agree, and there is no open roadmap PR or unresolved blocker.

Terminal exit: code, contract, tests, protected CI, release, security support statement, provenance, rollback, limitations, and cursor all agree. Only then may the S0-S6 execution goal be marked complete.

## Failure and rollback branches

- **PR #18 changed or failed:** do not merge; review the delta and rerun the S5 gate.
- **Post-merge S5 failure:** keep S6 inactive, correct S5 through a new reviewed PR, and reconcile the cursor afterward.
- **Candidate gate failure:** fix only the demonstrated defect on S6 and rerun the whole affected gate set.
- **Tag exists unexpectedly:** stop before mutation and reconcile ownership/commit identity.
- **Release validation fails after publication:** remove traffic from the release, deploy the confirmed S5 fallback, invalidate volatile sessions by restart, preserve audit evidence, and correct through a new version. Do not retarget the published `v1.0.0` tag.
- **Provider or contract drift:** stop; a provider upgrade or contract change requires a separate compatibility decision and cannot be folded into release closeout.
