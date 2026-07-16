# BLK-001: protected CI cannot read the private provider repository

Status: blocking S0 on 2026-07-16

## Evidence

- S0 PR: `drkliu/jimu-studio#1`
- Failed run/job: `29474611382` / `87544813507`
- Required command: `go run github.com/drkliu/jimu/cmd/jimuctl@v0.1.1-0.20260716034006-610cfc13c69f studio verify --dir contracts/studio/v1`
- GitHub Actions failed at `git ls-remote https://github.com/drkliu/jimu` with `could not read Username`; the Studio repository's scoped `GITHUB_TOKEN` has no read access to the private provider repository.
- The exact provider closeout source at `610cfc13c69f` verified the canonical artifact bundle locally. This is a CI credential failure, not artifact drift.
- The local GitHub CLI token has broad `repo` and `workflow` scopes. Persisting that token into a public repository's Actions secrets would violate least privilege and is not an acceptable autonomous workaround.

## Alternatives and scores

Scores use security (35), contract authority (30), operational reliability (20), and delivery cost (15). Options below 70 are unsafe or incompatible.

| Alternative | Score | Disposition |
|---|---:|---|
| Add a fine-grained `JIMU_READ_TOKEN` limited to contents-read on `drkliu/jimu`, then checkout commit `610cfc13c69f` and run its CLI locally in CI | 94 | Recommended; required credential is unavailable. |
| Authorize a read-only deploy key on `drkliu/jimu` and store only its private key in the Studio Actions secret | 91 | Equally safe; requires provider repository settings authority. |
| Publish an immutable provider release/module artifact containing the closeout verifier | 88 | Strong long-term fix; requires provider release authority not granted in this roadmap. |
| Store the current broad CLI OAuth token as a Studio Actions secret | 38 | Rejected: excessive cross-repository and workflow authority. |
| Reimplement or silently weaken the provider verifier in Studio CI | 25 | Rejected: duplicates provider authority and permits contract drift. |
| Make the provider repository public | 30 | Rejected without an explicit provider visibility decision. |

## Exact requested decision

Provide one of the following:

1. A fine-grained GitHub token named `JIMU_READ_TOKEN`, scoped only to `drkliu/jimu` repository contents-read; or
2. Explicit authority to install a read-only deploy key on `drkliu/jimu` and the corresponding private-key Actions secret on `drkliu/jimu-studio`; or
3. Explicit authority to publish a compatible Jimu provider release/module artifact.

After the credential is available, update CI to checkout the exact provider commit, run `go run ./cmd/jimuctl studio verify` against the vendored bundle, wait for all checks, protect `main`, merge S0, reconcile the cursor, and continue automatically with S1.
