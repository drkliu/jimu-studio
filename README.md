# Jimu Studio

Jimu Studio is the all-Go, accessible administration interface for Jimu. It uses a backend-for-frontend boundary so OIDC bearer tokens and tenant-scoped provider clients remain in volatile server memory and are never exposed to browser storage, URLs, fixtures, or logs.

## Pinned provider contract

- Provider: `github.com/drkliu/jimu`
- Provider commit: `bf130c33ba3d47bb7239d238b241b853df72f24e`
- Go pseudo-version: `v0.1.1-0.20260719044115-bf130c33ba3d`
- Studio contract: `1.2.0`
- OpenAPI fingerprint: `f797f4650bb62753dc09adb9a713b2b45a54c1b70b58365c9466a404d45ac52e`

The files in `contracts/studio/v1` are immutable generated artifacts. CI runs the pinned provider's `jimuctl studio verify` before application checks.

## Development

Go 1.26.5 is required.
Runtime setup and the loopback-only development boundary are documented in [Studio configuration](docs/configuration.md).

```text
go test ./...
go vet ./...
go run ./cmd/studio
go test -tags=e2e ./e2e
```

The browser suite uses Chrome DevTools through Go. No Node.js toolchain is used.

### Windows launcher

Run Studio from Command Prompt or by double-clicking `run-local.bat`. With no argument it reads `STUDIO_CONFIG`, or falls back to `studio.local.json` beside the launcher. A config path may be supplied explicitly:

```text
run-local.bat "D:\path\to\studio-config.json"
```

Run the complete local Go verification matrix with:

```text
run-local.bat test
```

For a complete no-Docker local login and operation stack, open three Command Prompt windows and run `run-oidc.bat`, `run-provider.bat`, then `run-local.bat`. The launchers bind Dex, the in-memory reference Provider, and Studio only to `127.0.0.1`. The local login is printed by `run-oidc.bat`. The local Provider is non-persistent test infrastructure and records each mutation for the Audit page.

## Release acceptance

The supported released baseline is [`v1.0.0`](https://github.com/drkliu/jimu-studio/releases/tag/v1.0.0). Its immutable machine record is archived at `release/history/v1.0.0-acceptance.json` and rendered in [the v1.0.0 acceptance record](docs/releases/v1.0.0-acceptance.md).

`release/acceptance.json` is now the protected-CI accepted `v1.1.0` record; publication remains unclaimed. Its scored operation matrix, evidence, limitations, and rollback are documented in [the v1.1.0 acceptance record](docs/releases/v1.1.0-acceptance.md). Every Provider operation is independently recorded and build-validated in [the operation scorecard](docs/releases/v1.1.0-operation-scorecard.json).

The packaging claim is source/commit only; no reproducible binary or container image is advertised.
