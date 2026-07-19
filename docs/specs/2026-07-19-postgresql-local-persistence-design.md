# PostgreSQL local persistence design

## Scope

Replace every local runtime `memory` storage authority with PostgreSQL while preserving the existing Studio contract, role controls, optimistic concurrency, idempotency, and audit behavior. No Studio browser route or Provider contract changes.

## Runtime design

The Provider owns schema `jimu_studio_local` in database `jimu_studio_local`. A singleton state row contains the bounded local fixture as JSONB plus a monotonic revision and database update time. Each authenticated request opens a transaction and locks the row before materializing request state. Successful operations serialize the complete state before commit; the HTTP response is buffered until the commit succeeds. Storage failures return a bounded `503 storage_unavailable` response and never expose a successful mutation.

The singleton is intentional for the native local test Provider: it gives atomic cross-domain changes and deterministic fixtures. It is not the production Jimu Provider storage schema. The production Provider remains the authority behind the Studio contract.

Dex owns database `jimu_dex_local` through role `jimu_dex` and uses the PostgreSQL storage adapter. Provider and Dex credentials share a local convenience secret but have different roles and databases. Production deployments should issue distinct managed credentials.

## Acceptance

- no `type: memory` in the active Dex configuration;
- Provider startup requires and verifies PostgreSQL;
- health reports PostgreSQL;
- schema creation and seed are repeatable;
- all local role operations pass against real PostgreSQL;
- entity create/delete/audit behavior passes against real PostgreSQL;
- a close/reopen integration test proves persistence;
- race, vet, build, browser, dependency, vulnerability, and CodeQL gates remain required;
- setup and rollback are documented and scored.

## Rollback

Revert the Studio source change and restore the prior local-only memory configuration. PostgreSQL databases are not deleted automatically and remain recoverable. A rollback loses access to persisted local state while the old binaries run but does not destroy it.
