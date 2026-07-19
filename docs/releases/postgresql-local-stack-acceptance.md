# PostgreSQL local stack acceptance

Status: locally accepted on 2026-07-19; protected CI and merge pending

## Scores

| Dimension | Score | Critical result |
|---|---:|---|
| Provider PostgreSQL persistence | 96 | atomic commit, fail-closed startup, and restart durability pass |
| Dex PostgreSQL persistence | 95 | memory backend removed; 11 tables and signing-key rotation verified |
| Local operability | 94 | Docker-free bootstrap passes twice and removes its transient SQL file |
| Authorization and audit regression | 97 | all role mutations and audit checks pass with the dedicated database role |
| Evidence and rollback | 96 | decision, design, plan, scorecard, and non-destructive rollback recorded |
| Mean | 95.6 | minimum 94; required minimum 90 |

The machine-readable companion is `postgresql-local-stack-scorecard.json`.

## Local evidence

| Gate | Result |
|---|---|
| PostgreSQL 17 bootstrap with dedicated roles/databases | pass twice; zero transient SQL leftovers |
| Provider PostgreSQL integration tests | pass |
| Provider persistence across close/reopen | pass |
| Full `go test -race -count=1 ./...` | pass |
| `go vet ./...` and both command builds | pass |
| PostgreSQL-backed Chrome E2E | pass in 75.405s |
| Dex PostgreSQL storage | pass; migrations, signing keys, 11 tables, and discovery issuer verified |
| `govulncheck ./...` | pass; no vulnerabilities found |
| `git diff --check` | pass |

The database tests used an isolated PostgreSQL 17 cluster on loopback port 55432. It was stopped and its validated temporary directory removed after testing. The user's existing service on port 5432 was not restarted or modified because its administrator credential was not available to the process.

## Production boundary

This acceptance covers the native loopback Provider/Dex stack used to exercise the production Studio UI and contract. The singleton JSONB state is intentionally bounded local-fixture storage, not a replacement for the production Jimu Provider schema. Production deployments require TLS to PostgreSQL, distinct managed credentials, backups, monitoring, and the production Provider implementation.
