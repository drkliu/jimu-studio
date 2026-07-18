# Jimu Studio S5-S6 Release Closeout Design

Status: accepted and executing on 2026-07-18

## Purpose

This document is the complete specification for the work remaining after S0-S4. It does not treat an open pull request, a green pre-merge run, a tag, or a draft release as completed work. The closeout has two ordered gates:

1. reconcile and merge the already-implemented S5 operations console; and
2. complete S6 release acceptance on the resulting protected `main` commit.

No additional product capability is authorized by this closeout. The implementation remains an all-Go, server-rendered BFF. The generated TypeScript contract client remains verification evidence and is not an application dependency.

## Reconciled baseline

Live state checked on 2026-07-18 (Asia/Kuala_Lumpur):

| Item | Grounded state |
|---|---|
| Provider authority | `github.com/drkliu/jimu` commit `0a9a8c662f2b8290c3f911c97b0a80eb55d560d0` |
| Provider pin | `v0.1.1-0.20260716150217-0a9a8c662f2b` |
| Studio contract | `1.1.0`; OpenAPI source fingerprint `3a5a4bb8e35cb66ff3374f7a28d5d401684fcc56441aca85df75d64c8c922f19` |
| Studio S5 merge | PR #18 merged at `f02a750141b122703e57e6c715adf1c98c0141ee` |
| S5 protected evidence | CI run `29639714157` and CodeQL run `29639714159`, successful |
| S5 reconciliation | PR #19 merged at `61c5b7cd711d5516472aa0a3718e711f314caebc` |
| Reconciliation evidence | CI run `29639992477` and CodeQL run `29639992443`, successful |
| Roadmap cursor | S5 reconciled; S6 active on `feature/s6-release-acceptance` |
| Release | none observed; `SECURITY.md` therefore correctly says supported releases are not yet published |

The first PR #18 merge attempt failed without mutation because protection still required obsolete `Contract 1.0.0`. Protection was safely changed by adding `Contract 1.1.0` before removing only the obsolete context. The unchanged green PR then merged once and was read back before reconciliation.

## S5 terminal contract

Status: satisfied through PRs #18 and #19 with the protected evidence above.

S5 is complete only when all of the following are true:

- PR #18 is merged once into protected `main` and the exact squash commit is known;
- the merge commit contains the contract 1.1.0 bundle and the workflow, quota, discrepancy, usage, and audit console implementation;
- protected post-merge CI and CodeQL succeed for that exact `main` commit;
- the cursor records S5 as the last merged slice, records the exact merge commit and post-merge run IDs, and activates S6 on a new branch;
- no diagnostic files, stale 1.0.0 release metadata, or uncommitted generated-artifact drift enters the merge or S6 branch.

If PR #18 is still open and its head or checks differ from the baseline above, the merge pauses for a new review. The prior authorization applies only to the recorded PR #18 administrator squash merge, not to an altered head, S6, a tag, or a release.

## S6 release identity

The first supported Studio release is `v1.0.0`. Its identity is the annotated Git tag, the immutable GitHub release, and the full commit SHA to which both point. The release commit must be a descendant of the confirmed S5 merge and must contain only release-acceptance changes beyond S5 unless a failing gate proves a narrowly scoped corrective change is necessary.

Release metadata must record:

- Studio version, release tag, and full release commit;
- Go version `1.26.5`;
- provider repository, full commit, and Go pseudo-version;
- Studio contract version, OpenAPI source fingerprint, and every contract artifact SHA-256 digest;
- protected acceptance run IDs and conclusions for contract, formatting, vet, unit/race, build, vulnerability, dependency review, CodeQL, and Go browser E2E;
- the supported deployment and browser matrix;
- known limitations and the rollback target/procedure.

Release metadata is factual evidence, not a prediction. Tag, release URL, timestamps, and release-run IDs remain absent or explicitly pending until those objects exist.

## Acceptance matrix

The release candidate is accepted only when every row is green on the same candidate commit.

| Area | Required evidence |
|---|---|
| Contract | Run the pinned provider's `jimuctl studio verify --dir contracts/studio/v1`; validate manifest version, source fingerprint, LF bytes, digests, and absence of extra artifacts. |
| Go source | `gofmt` reports no files; `go vet ./...`; `go test ./... -count=1`; `go test -race ./... -count=1`; and `go build ./cmd/studio` pass with Go 1.26.5. |
| Browser/accessibility | Protected Linux `go test -tags=e2e -timeout=2m ./e2e` passes against Chrome. The suite must cover semantic structure, keyboard operation, tenant switching, optimistic conflict, and every dangerous confirmation path. |
| Authentication and isolation | OIDC Authorization Code + PKCE, signed identity verification, single-use state/nonce, server-only tokens, fresh tenant clients, cache/draft disposal, CSRF, and provider redirect rejection remain covered. |
| Provider operations | Metadata plan/apply, users/roles, workflow cancel/task retry, quota publication, usage/discrepancy views, and redacted audit retain bounded requests, role gates, stable server-generated idempotency keys, expected versions, and 401/403/409 behavior. |
| Supply chain | Dependency review, vulnerability scanning, module verification, and CodeQL succeed; the private provider checkout remains pinned with persisted credentials disabled. |
| Documentation | README, configuration, security support statement, release acceptance, known limitations, rollback, and provenance agree with the release commit. No document claims a release before it exists. |

Windows browser startup is not release authority. A local Windows compile check is useful, while protected Linux browser CI is the browser acceptance record.

## Supported deployment and browser matrix

- Studio runs as a single Go process behind a production TLS termination boundary.
- Production Studio, OIDC issuer, redirect, and provider endpoints use HTTPS. Development HTTP is limited to explicit loopback configuration.
- Configuration is supplied through `STUDIO_CONFIG`; OIDC client secrets are referenced through environment-variable names and are not stored in the JSON file.
- Chromium/Google Chrome matching the protected Ubuntu runner is the tested browser family for `v1.0.0`.
- Keyboard navigation and semantic HTML are release requirements. A formal claim against a named WCAG conformance level is out of scope until an independent audit exists.

## Known limitations

The `v1.0.0` release notes and committed acceptance record must disclose these limits:

1. Sessions, OIDC transactions, tokens, provider clients, caches, drafts, and idempotency state are volatile process memory. Restart logs users out; multi-instance deployment requires session affinity and still does not provide shared session failover.
2. TLS termination, secret injection, process supervision, backup of external provider data, and provider HTTP handler bindings are deployment-owner responsibilities. Studio does not terminate public TLS or store provider data.
3. The UI is contract-bound to Studio `1.1.0` at the pinned provider commit. It does not negotiate arbitrary contract versions or silently accept artifact drift.
4. Chrome on protected Linux CI is the browser authority. Other Chromium builds may work but are not release-certified; Firefox and Safari are not certified for `v1.0.0`.
5. Metadata relation visualization/editing is limited to fields present in the frozen contract. Studio does not infer relationships from names or undocumented provider behavior.
6. Usage and discrepancy pages are observational accounting views, not synchronous quota-enforcement authority.
7. Audit details are intentionally not rendered or exported; only bounded redacted fields and `redacted_paths` are shown.
8. Studio provides no password, token, certificate, raw SQL, workflow payload/result, worker identity, or fencing-token administration surface.
9. Release packaging is source/commit based unless the release PR explicitly adds separately verified binaries. GitHub-generated archives alone are not claimed as reproducible application binaries.

## Rollback contract

Rollback is an operational deployment action, not a mutation of provider data:

1. Stop routing new traffic to the rejected Studio release and preserve provider/audit evidence.
2. Redeploy the last accepted Studio commit. For the first release, the fallback is the confirmed S5 merge commit, which has the same application capability but is not advertised as a supported release.
3. Restart Studio. All volatile sessions, tokens, drafts, pending OIDC exchanges, cached pages, and stable mutation keys are intentionally discarded; users authenticate again.
4. Keep the provider pinned to the contract-compatible commit. Do not downgrade the provider or contract artifacts as part of a Studio-only rollback without a separately verified compatibility decision.
5. Re-run health, contract verification, authentication, tenant-switch isolation, and one read-only operation per visible area before restoring traffic.

Studio rollback does not reverse metadata applications, identity mutations, workflow commands, or quota publications already accepted by the provider. Those are provider-authoritative, audited operations and require their own compensating procedure.

## Provenance and release publication

The release candidate is built and tested from a clean protected commit. The final acceptance record is generated from committed inputs plus GitHub run evidence and is reviewed in the S6 PR. After that PR is merged and protected post-merge checks are green, the tag and GitHub release may be created for that exact commit.

Publication is terminal only when:

- the tag resolves to the intended protected `main` commit;
- the GitHub release is non-draft and non-prerelease;
- release notes contain the acceptance summary, supported matrix, known limitations, rollback procedure, provider/contract pins, and provenance link;
- `SECURITY.md` names `v1.0.0` as supported without promising an unapproved remediation SLA;
- the cursor records the real release tag, URL, commit, timestamp, and evidence, sets S6 as last merged, clears the active branch/slice, and marks the roadmap closed.

Creating or publishing the tag/release is an external mutation and requires explicit authorization after the exact candidate commit and evidence are presented. Authorization to merge PR #18 does not authorize S6 merge, tag creation, or release publication.

## Closeout and future work

After publication, no S0-S6 item remains. New browsers, shared session storage, additional packaging, provider upgrades, contract changes, new UI capability, or a formal accessibility certification require a new scored spec and plan. They are not silently inherited by this roadmap.
