# PostgreSQL local persistence implementation plan

Status: complete on 2026-07-19

| Step | Evidence | Score (0-100) | Gate |
|---|---|---:|---|
| Replace Provider memory authority with transactional PostgreSQL | integration and restart tests | 96 | minimum 90 and no durability failure |
| Replace Dex memory storage with PostgreSQL | config inspection and live discovery/login | 95 | dedicated database and no committed secret |
| Add direct Windows setup/start workflow | clean rerun of `setup-postgres.bat` and health checks | 94 | no Docker, loopback connection, actionable errors |
| Preserve all Studio operations and audit behavior | full Go/race/browser suites and operation scorecard | 97 | no operation regression |
| Record security, rollback, and production boundary | DEC-009, design, configuration, acceptance record | 96 | all records present and consistent |

The loop is: implement, format, run PostgreSQL integration tests, start Provider and Dex, verify persistence/login/UI, run all quality gates, review the diff, open a pull request, wait for protected checks, merge, and record immutable evidence. Any score below 90 or failed critical gate returns the loop to implementation.
