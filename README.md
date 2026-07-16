# Jimu Studio

Jimu Studio is the all-Go, accessible administration interface for Jimu. It uses a backend-for-frontend boundary so OIDC bearer tokens and tenant-scoped provider clients remain in volatile server memory and are never exposed to browser storage, URLs, fixtures, or logs.

## Pinned provider contract

- Provider: `github.com/drkliu/jimu`
- Provider commit: `610cfc13c69f`
- Go pseudo-version: `v0.1.1-0.20260716034006-610cfc13c69f`
- Studio contract: `1.0.0`
- OpenAPI fingerprint: `e41ab114195abcf5791ae1f3d4eb402d1c1877d69d4c863e64880f5b79f0bf91`

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
