# Jimu Studio S5-S6 Release Closeout Loop

Spec: `docs/specs/2026-07-18-s5-s6-release-closeout-design.md`

Plan: `docs/plans/2026-07-18-s5-s6-release-closeout.md`

Current grounded cursor: terminal. S5 merged through PR #18; reconciliation PR #19 merged; S6 merged through PR #20; evidence PR #21 produced release commit `2db2c8bcd877174c068f65ed034303c876da7834`; annotated tag and non-draft, non-prerelease release `v1.0.0` were read back at that commit. Final cursor closeout PR #22 merged at `de533b4a86d4c44101a8619d41f072b3e8e2b1a5`; protected CI `29640948113` and CodeQL `29640947998` succeeded. The terminal objective is proven.

## Terminal objective

Run the plan until all S5-S6 work is complete and reconciled: S5 is merged once, S6 acceptance is merged, `v1.0.0` is published from the accepted protected commit, final release facts are merged, protected post-merge checks are green, and the roadmap cursor is closed.

Implementation, documentation, tests, review, protected CI, remote state, and cursor evidence must agree. Local code, a green feature run, an open PR, a tag without a release, or a release without final cursor reconciliation is not completion.

## Master loop

```text
load spec, plan, cursor, handoff, contract manifest, release metadata

while terminal objective is not proven:
    reconcile local and remote truth
    derive the earliest incomplete phase from observed evidence
    select the next dependency-ready task in that phase

    if task is read-only reconciliation:
        inspect and record evidence
        continue

    if task is a local or branch implementation task:
        confirm branch descends from the required protected commit
        preserve unrelated work and generated contract authority
        record any material deviation before implementation
        capture RED when behavior or validation changes
        implement the smallest plan-conforming change
        reach GREEN and refactor
        run targeted verification
        review security, isolation, compatibility, rollback, and evidence
        update only facts already proven
        continue

    if task changes remote state:
        reconcile the exact remote target again
        prove prerequisites and identify the exact commit/head
        require authorization specific to this mutation
        perform the mutation once
        read the result back before updating any cursor or release fact
        wait for the required protected checks
        continue

    if a gate fails:
        preserve failure evidence
        classify the failure as product, test, environment, contract, or remote state
        return only the affected phase to in_progress
        fix the demonstrated cause through a reviewed branch
        rerun the affected gates and every downstream invalidated gate
        continue

    if safe progress is blocked:
        record the exact blocker, attempted alternatives, and required decision
        finish any independent read-only or local work
        pause without fabricating completion

verify terminal objective directly from protected main, GitHub release state,
post-merge runs, committed acceptance/provenance, and the closed cursor
```

## Phase derivation

Always choose the first row whose exit proof is absent. Later work may be prepared locally only when it cannot accidentally claim or depend on an unproven earlier remote fact.

| Order | Phase | Entry condition | Exit proof |
|---:|---|---|---|
| 1 | S5 merge | PR #18 exists and S5 is not proven on `main` | exact merge commit on protected `main` plus green post-merge CI and CodeQL |
| 2 | S5 reconciliation | S5 merge proof exists; cursor/provider metadata is stale | reconciliation PR merged, contract 1.1.0 metadata exact, cursor activates S6, post-merge checks green |
| 3 | S6 candidate | reconciled S5 protected commit exists | release acceptance, limitations, rollback, provenance schema, documentation, and validators implemented and locally green |
| 4 | S6 acceptance | S6 candidate branch is complete | focused S6 PR merged and exact protected commit has all required green checks |
| 5 | Release publication | accepted untagged S6 commit exists | annotated `v1.0.0` tag and non-draft/non-prerelease GitHub release both resolve to that commit and are read back |
| 6 | Cursor closeout | release facts exist | final closeout PR merged; release/security/cursor facts agree; post-merge checks green; no active slice remains |

Do not advance a phase from expected state. Advance only from observed exit proof.

## State machine

```text
pending -> ready -> in_progress -> verifying -> reviewed -> authority_wait
   ^          ^          |              |             |
   |          |          +---- failed <-+             |
   |          |                                      authorized
   |          +------------------------------------------+
   |                                                     v
   +---------------- correction <- merged <- mutating_remote
                                      |
                                      v
                                     done
```

- `pending`: an earlier phase lacks exit proof.
- `ready`: all hard dependencies are observed and the task can start safely.
- `in_progress`: branch-local implementation or evidence work is active.
- `verifying`: targeted or full gates are running.
- `reviewed`: required findings are resolved and the exact mutation target is known.
- `authority_wait`: the next step is an external mutation without specific authorization.
- `mutating_remote`: one authorized merge, tag, or release operation is in flight.
- `merged`: the expected remote object was read back, but protected evidence or reconciliation remains.
- `done`: the phase exit proof is committed and reconciled.
- `failed`: a gate or read-back disagrees with the expected result; never reinterpret this as success.
- `correction`: a new reviewed change restores the failed phase without rewriting published history.

Only observed evidence moves a task forward. A timeout during remote mutation stays `mutating_remote` until a read-only reconciliation proves success or failure; never repeat the mutation merely because the client timed out.

## Reconciliation pass

At the start of every loop iteration and after every remote mutation, read:

1. local branch, HEAD, worktree, remotes, and recent ancestry;
2. protected Studio `main` commit;
3. PR #18 and any S6/closeout PR state, head, merge commit, and required checks;
4. tag and GitHub release state for `v1.0.0`;
5. `docs/roadmap/cursor.json`, `release/provider-contract.json`, the frozen contract manifest, README, SECURITY, configuration, spec, plan, and acceptance record;
6. protected CI and CodeQL runs attached to the exact commit under evaluation.

Prefer the codebase graph for symbol, call-path, and impact discovery. Text search is appropriate for JSON, Markdown, workflow YAML, exact artifact values, and remote IDs.

When sources disagree, use this authority order:

```text
GitHub remote object/read-back
    -> protected commit contents and ancestry
    -> protected CI/CodeQL attached to that commit
    -> frozen provider contract and manifest
    -> live code and tests
    -> committed cursor/release evidence
    -> historical plans, handoffs, and chat
```

Lower-authority records are corrected through a reviewed commit; higher-authority state is not rewritten to match stale documentation.

## Implementation protocol

For every plan task that changes behavior or machine-validated evidence:

1. Identify the exact requirement and affected code path.
2. Add or adjust a test that fails for the missing behavior or invalid metadata.
3. Capture the RED result without weakening unrelated tests.
4. Implement the smallest coherent change using Go 1.26.5.
5. Reach GREEN, then refactor while retaining contract fidelity.
6. Run focused tests, formatting, vet, race tests, build, and applicable browser/security gates.
7. Review tenant isolation, OIDC/CSRF boundaries, bounded inputs, redirects, roles, idempotency, optimistic versions, confirmations, redaction, secrets, and raw/internal provider data.
8. Record deviations, evidence, rollback effect, and any remaining limitation.

Documentation-only factual updates do not require an artificial failing unit test, but their JSON/schema validators and link/value consistency checks must pass.

## Mandatory gate loop

```text
repeat:
    verify pinned provider contract 1.1.0 byte-for-byte
    require clean gofmt output
    run go test ./... -count=1
    run go test -race ./... -count=1
    run go vet ./...
    run go build ./cmd/studio
    run module, dependency, vulnerability, and CodeQL-equivalent checks
    compile the Go browser suite
    run browser E2E where Chrome is available
    review the complete diff and provenance values

    if every local gate is green:
        break
    fix the demonstrated cause and repeat

push one coherent branch
wait for protected Linux contract, quality, browser, dependency,
vulnerability, and CodeQL checks on the exact head
```

Protected Linux browser CI is authoritative when local Windows Chrome cannot start. An environmental local browser failure must still be recorded and the suite must at least compile; it does not permit skipping protected browser CI.

## Remote mutation protocol

Apply this protocol separately to PR #18 merge, S5 reconciliation merge, S6 merge, tag push, GitHub release creation, and closeout merge:

```text
read target state
if desired state already exists:
    verify identity and consume it; do not mutate again
else:
    verify exact head/commit, checks, ancestry, and absence of conflicts
    verify authorization names this mutation
    perform exactly one mutation

read target state again
if read-back proves success:
    record exact immutable identifiers
elif read-back proves failure:
    preserve evidence and return to the appropriate phase
else:
    remain unresolved and retry read-only reconciliation only
```

Authorization is never inherited:

- the recorded PR #18 administrator squash authorization applies only while its head remains the reviewed `fe5d297478c7cbada4ff329ecca8806680d0fd88`;
- it does not authorize an altered PR #18, the reconciliation PR, S6 PR, closeout PR, tag push, or release publication;
- tag creation and GitHub release publication must be presented together with the exact accepted commit and release evidence, then explicitly authorized.

## Failure loop

| Failure | Loop response |
|---|---|
| PR/head changed | Return to reconciliation and review the entire new delta; previous merge authorization is invalid. |
| Required check failed | Diagnose from the exact run, correct through a new commit, and rerun all invalidated checks. |
| Merge/tag/release command timed out | Perform read-only remote reconciliation; never issue a duplicate mutation until failure is proven. |
| Protected post-merge failure | Keep the phase incomplete, correct through a new PR, and record both failure and recovery evidence. |
| Contract or manifest drift | Stop feature/release work; restore the pinned authority or create a separate compatibility decision. |
| Stale cursor/release JSON | Correct it only from observed remote/manifest facts and validate it before merge. |
| Tag exists at another commit | Stop. Do not force, delete, or retarget published history without a separately approved recovery plan. |
| Published release is defective | Remove traffic, deploy the confirmed S5 fallback, invalidate volatile sessions by restart, preserve audit evidence, and issue a new version after correction. |
| Local environment cannot run a gate | Exhaust safe workspace-local configuration, record the limitation, and use protected CI only where the spec explicitly makes it authoritative. |

## Progress record

After each completed task, record or report:

- phase and task;
- before/after local and protected commit IDs;
- files and behavior changed;
- RED/GREEN or documentation-validation evidence;
- local and protected gate results;
- PR, merge, tag, release, and run IDs that actually exist;
- authorization consumed, if any;
- limitations, rollback impact, and next dependency-ready task.

Never place tokens, OIDC secrets, provider secrets, raw SQL, tenant data, workflow payload/results, worker identity, fencing values, or unredacted audit details in the progress record.

## Stop and completion conditions

Pause only when the next necessary step requires missing authority, a changed reviewed target, an unavailable external dependency with no independent safe work, irreconcilable accepted requirements, or a material scope expansion outside the spec. State the exact decision or authority needed.

Do not stop merely because a task is slow, CI is pending, a local optional gate is unavailable, or a correction is required. Wait/reconcile and continue through the loop.

The loop terminates only after direct read-back proves all of the following:

1. S5 and S6 are ancestors of protected `main`.
2. Required post-merge CI and CodeQL are green on the reconciled commits.
3. `v1.0.0` is an annotated tag and a non-draft, non-prerelease GitHub release at the accepted commit.
4. The committed release acceptance, provenance, limitations, rollback, support statement, provider metadata, and contract manifest agree.
5. The final cursor records S6/release facts, has no active slice or branch, and is merged.
6. The Studio worktree is clean and no unresolved S0-S6 blocker or roadmap PR remains.

If any item is absent, select the earliest responsible phase and continue.
