# Full role-governed operations implementation plan

Status: in progress on 2026-07-19

1. [x] Reconcile current Provider/Studio code, roles, previous decisions, local stack, and score methodology.
2. [x] Add and merge Provider contract 1.2.0 deletion preview/delete operations with deterministic artifacts and full Provider CI.
3. [x] Pin Studio to the merged Provider commit, contract fingerprint, and artifact digests.
4. [x] Implement explicit entity creation with version-zero semantics and accessible dynamic fields.
5. [x] Implement server-held deletion preview/apply with dependency blocking, exact confirmation, conflict invalidation, replay rejection, and receipts.
6. [x] Expand the native no-Docker Provider to all metadata, identity, workflow, quota, and audit operation families.
7. [x] Record and machine-validate every contract operation, role, evidence link, critical control, and production score.
8. [ ] Complete full unit/race/vet/build/contract/vulnerability tests and record results.
9. [ ] Complete authenticated browser create/delete plus accessibility and role-denial tests.
10. [ ] Commit, open the Studio PR, pass protected CI/CodeQL/dependency review, merge, verify protected main, and finalize the score/evidence record.

The loop stops on any score below 90 or any failed critical control. Fixes return to the earliest affected step and all downstream evidence is rerun.
