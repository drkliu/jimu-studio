# DEC-007: S6 evidence-bound v1.0.0 release acceptance

Status: accepted on 2026-07-18

## Reconciliation and decision

S5 merged through PR #18 at `f02a750141b122703e57e6c715adf1c98c0141ee`; protected CI `29639714157` and CodeQL `29639714159` succeeded. Cursor/provider reconciliation merged through PR #19 at `61c5b7cd711d5516472aa0a3718e711f314caebc`; protected CI `29639992477` and CodeQL `29639992443` succeeded. Contract 1.1.0 remains pinned to provider `0a9a8c662f2b` and no Studio release existed at S6 start.

The selected release model is an annotated `v1.0.0` source/commit release with a machine-validated acceptance record. Candidate state contains no invented tag commit, URL, publication time, or S6 run IDs. Final release state is written only after GitHub read-back and is merged through a closeout PR. No binary or container-image reproducibility claim is made.

## Safety consequences

- The validator cross-checks provider identity, contract version/fingerprint/digests, baseline evidence, required gates, supported deployment/browser scope, nine known limitations, and rollback semantics.
- Candidate records fail if they claim remote release facts or completed S6 evidence early. Released records fail unless the source commit, URL, RFC 3339 publication time, and every gate are complete and successful.
- Rollback redeploys the S5 application commit and invalidates volatile sessions; it never claims to reverse provider-authoritative mutations.
- Chrome on protected Ubuntu CI is authoritative. No formal WCAG level, unsupported browser, shared-session failover, binary, container, or remediation SLA is claimed.
- PR merges, tag creation, and release publication remain separate read-back-verified remote mutations.

## Verification plan

Run the pinned provider verifier, release/contract tests, full unit and race suites, formatting, vet, build, vulnerability/dependency checks, CodeQL, and protected Go browser E2E. Merge the S6 candidate only after all checks succeed, publish only the accepted commit, then finalize observed release facts and the cursor through a separate green closeout PR.
