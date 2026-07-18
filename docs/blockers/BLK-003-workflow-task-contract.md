# BLK-003: Studio 1.0.0 cannot safely retry or describe workflow tasks

Status: resolved on 2026-07-16 by provider contract 1.1.0 at `0a9a8c662f2b`

## Reconciled evidence

- Provider authority is `github.com/drkliu/jimu` main at `610cfc13c69fc007cc835613d5f6302b42d94fa0`; there are no open provider PRs.
- Studio main is clean at `639dc528c2b4022a1abc9a3ea4e1971c0a9ad587`; S4 merged at `a4c1d6f4ac7841ce39be6d9cf88fcedf7413add6`, there are no open Studio PRs, and the cursor activates S5.
- Pinned contract `1.0.0` verifies before implementation. Its OpenAPI digest is `9891172a3d4fa0f75b37a820d481c58e5e68cf12d4bede1cbf07959b0763549f`.
- The accepted roadmap requires run/task visibility; state, timing, and error summaries; exact `CANCEL_RUN` and `RETRY_TASK` confirmations; concurrent-update handling; and lease/recovery-state tests. The consolidated S5 also includes quota and audit administration.
- Studio 1.0.0 exposes bounded `GET /studio/v1/workflows/runs`. Each item contains only `id`, `workflow`, `state`, `version`, and `updated_at`.
- `POST /studio/v1/workflows/runs/{id}/cancel` is grounded by that list and requires `CANCEL_RUN`, the run's `expected_version`, and an idempotency key.
- `POST /studio/v1/workflows/tasks/{id}/retry` requires `RETRY_TASK`, the task's `expected_version`, and an idempotency key, but 1.0.0 exposes no task list or read operation and no task wire shape.
- The contract contains no task ID discovery, task version, timing, redacted error summary, lease status, or recovery status. The retry response is run-shaped and cannot supply the missing precondition before the request.

Therefore Studio cannot discover a task or obtain the authoritative version needed for optimistic concurrency. Reusing a run version, accepting an operator-supplied version, inferring task fields, or adding UI-local API shapes would invent provider semantics and could retry the wrong stale task. Quota and audit work is contract-ready, but it cannot close the accepted combined S5 while this workflow requirement remains.

## Scored alternatives

Scores use contract fidelity (35), accepted-requirement coverage (30), concurrency/security safety (20), and accessibility/testability (15). Work below 70 is rejected.

| Alternative | Score | Disposition |
|---|---:|---|
| Publish provider-owned Studio 1.1 with a bounded task list/read shape containing task ID, run ID, state, task version, timing, redacted error summary, and lease/recovery status; retain retry with the task version | 96 | Recommended: satisfies the accepted workflow slice without guessed concurrency semantics. |
| Explicitly amend S5 to runs-only visibility/cancellation and remove task visibility, retry, timing/error, and lease/recovery acceptance criteria | 76 | Safe only after requirement-owner approval; materially reduces the accepted release scope. |
| Implement quota and audit now while leaving S5 open for the provider extension | 80 | Safe partial progress only if explicitly authorized; it does not unblock or close S5. |
| Ask the operator to enter a task ID and expected version | 42 | Rejected: ungrounded UX, inaccessible error recovery, and unsafe optimistic concurrency. |
| Reuse the parent run version as the task expected version or infer task details | 10 | Rejected: undocumented contract behavior and likely stale/wrong-target mutation. |
| Modify the copied 1.0.0 artifacts in the UI repository | 0 | Rejected: violates provider authority and the pinned manifest. |

Every presently executable option that preserves all accepted S5 requirements scores below 70.

## Exact decision required

Choose one:

1. Keep the accepted workflow requirements: authorize a provider-owned Studio 1.1 contract extension with bounded task discovery/read data, authoritative task versions, timing, redacted errors, and lease/recovery status; publish its manifest and update the handoff before Studio repins. This is recommended.
2. Amend S5 and release acceptance to runs-only visibility/cancellation under Studio 1.0.0, explicitly removing task visibility/retry/timing/error/lease/recovery requirements.

Separately, confirm whether quota and audit partial work may proceed while option 1 is pending. It will remain an incomplete S5 and will not be released as roadmap completion.

No task workflow implementation or contract workaround will proceed until one decision is recorded.

## Resolution

Provider contract 1.1.0 was published and merged with bounded task discovery, authoritative task versions, timing, redacted error codes, and lease/recovery state. Studio repinned the generated artifact bundle byte-for-byte and recorded the implementation constraints in DEC-006.
