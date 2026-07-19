# DEC-009: PostgreSQL is the local persistence authority

Status: accepted for implementation on 2026-07-19

## Decision and scoring

Scores use durability and correctness (30), security/isolation (25), operability (20), testability (15), and implementation fit (10). A critical persistence or secret-handling failure rejects an option regardless of score.

| Option | Durability | Security | Operability | Testability | Fit | Total | Decision |
|---|---:|---:|---:|---:|---:|---:|---|
| Dedicated PostgreSQL databases for Provider and Dex | 30 | 23 | 18 | 15 | 9 | 95 | Selected |
| One shared PostgreSQL database and owner | 29 | 14 | 18 | 14 | 9 | 84 | Rejected: weak blast-radius isolation |
| Provider JSON files plus Dex memory storage | 12 | 17 | 14 | 12 | 8 | 63 | Rejected: partial durability and unsafe concurrent writes |
| In-memory Provider and Dex | 0 | 18 | 18 | 12 | 10 | 58 | Rejected: all state is lost on restart |

## Consequences

- The native local Provider fails closed when `JIMU_STUDIO_POSTGRES_DSN` is absent or PostgreSQL is unavailable.
- Provider state is loaded and saved inside a database transaction with a row lock. Resource mutations, optimistic versions, idempotent results, and audit records commit atomically.
- Each Provider test receives a unique PostgreSQL schema, and restart coverage proves that committed metadata survives process recreation.
- Dex uses its supported PostgreSQL backend with a dedicated role/database and automatic Dex migrations.
- Local scripts never commit a database password. They accept environment-injected secrets and restrict the convenience DSN to URL-safe passwords.
- The local stack remains loopback-only and Docker-free. CI may use an ephemeral PostgreSQL service as test infrastructure.
