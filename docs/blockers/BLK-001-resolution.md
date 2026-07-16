# BLK-001 resolution

Resolved on 2026-07-16 after explicit user authorization.

- A fresh passphrase-free Ed25519 deploy key was generated and proved with an authenticated read-only `git ls-remote` before storage.
- Public key ID `157442141` was installed read-only on private `drkliu/jimu` as `jimu-studio-contract-ci`.
- The private key was stored only as encrypted Actions secret `JIMU_PROVIDER_DEPLOY_KEY` in `drkliu/jimu-studio`.
- Temporary local private/public key files were deleted immediately after installation.
- S0 CI checks out exact provider commit `610cfc13c69f` using the deploy key and runs that checkout's `jimuctl` against the pinned Studio 1.0.0 bundle.
- The deploy key grants no write access and is dedicated to this one contract-verification path.
- An initial encrypted/passphrase-mismatched key was rejected by CI, removed from both repositories, and replaced; no stale credential remains.
