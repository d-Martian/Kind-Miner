# Project Surfaces

Use this reference for the known XMRig/P2Pool integration surfaces in this app.

## Version Pins

Current dependency pins live in three kept-in-sync places:

- `internal/autoinstall/deps.json` — source of truth for the runtime path
  (embedded in the binary, verified at download time).
- `scripts/download-xmrig.sh`
- `scripts/download-p2pool.sh` — platform asset URLs and SHA256 sums for release
  packaging.

`apply_dependency_update.py` updates all three from `latest.json`. Note XMRig
publishes no Linux ARM64 prebuilt, so `linux-arm64` is intentionally absent from
the XMRig pins (it would otherwise permanently trip the missing-asset gate).

## Runtime Autoinstall

Runtime download logic lives in:

- `internal/autoinstall/autoinstall.go`

Important behavior:

- `EnsureXMRig` and `EnsureP2Pool` call `ensurePinned`, which downloads the
  version pinned in `deps.json` and verifies the archive SHA256 before
  extraction — the same tamper-evident, reproducible pattern as Tor.
- There is no longer any `/releases/latest` following at runtime; moving versions
  requires editing `deps.json` (via a reviewed PR).
- Tor remains version-pinned and SHA256-verified inline in the same file.

When bumping versions, `deps.json` must change too, or the runtime will keep
fetching the old pinned build. `apply_dependency_update.py` handles this.

## Process Integration

Core startup uses:

- `internal/core/core.go`
- `internal/engine/xmrig.go`
- `internal/engine/p2pool.go`
- `internal/scheduler/scheduler.go`

Review these files if upstream changes touch command-line flags, startup readiness, pause/resume behavior, thread control, local ports, Stratum behavior, or P2Pool node requirements.

## Tests And Builds

Useful checks:

- `go test ./...`
- `make verify-repro`
- `bash scripts/download-xmrig.sh`
- `bash scripts/download-p2pool.sh`
- package-specific smoke tests if AppImage or Flatpak manifests are touched.

Do not assume GUI/cgo cross-compilation is reproducible from one host. Follow `REPRODUCIBLE.md`.

