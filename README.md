# Jimu Studio

Jimu Studio is the all-Go, accessible administration interface for Jimu. It uses a backend-for-frontend boundary so OIDC bearer tokens and tenant-scoped provider clients remain in volatile server memory and are never exposed to browser storage, URLs, fixtures, or logs.

## Pinned provider contract

- Provider: `github.com/drkliu/jimu`
- Provider commit: `0a9a8c662f2b`
- Go pseudo-version: `v0.1.1-0.20260716150217-0a9a8c662f2b`
- Studio contract: `1.1.0`
- OpenAPI fingerprint: `3a5a4bb8e35cb66ff3374f7a28d5d401684fcc56441aca85df75d64c8c922f19`

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

## Release acceptance

The proposed first supported release is `v1.0.0`. Its machine-readable candidate record is `release/acceptance.json`, with the rendered acceptance matrix, known limitations, rollback procedure, and provenance in [the v1.0.0 acceptance record](docs/releases/v1.0.0-acceptance.md).

No tag or release is claimed until the GitHub objects exist and the final closeout record is merged. The current packaging claim is source/commit only; no reproducible binary or container image is advertised.
