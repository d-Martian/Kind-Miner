# Reproducible Builds

Use this reference to keep XMRig/P2Pool updates aligned with reproducible-builds.org practices.

## Principles

A reproducible build means the same source code, build environment, and build instructions can recreate bit-for-bit identical artifacts. The project should define exactly which dependency binaries and packaging outputs are part of that claim.

For dependency updates:

- pin versions,
- pin SHA256 hashes,
- avoid mutable "latest" inputs in release packaging,
- keep the supported platform asset set explicit,
- record the upstream release URL and asset names,
- run reproducibility checks after the update,
- document any limits, especially native cgo or platform SDK caveats.

## Stable Inputs

Dependency archives are build inputs. They must be stable:

- exact release tag,
- exact asset filename,
- exact SHA256,
- exact extraction target.

If an upstream changes asset naming or removes a platform, do not paper over it. Mark it as a compatibility issue and decide whether to change support policy or build that dependency from source.

## Checksums

Prefer both:

- upstream published hashes/signatures when available,
- locally computed SHA256 for the exact archive URL this project will download.

The local project should pin the locally computed SHA256 in its download scripts or manifest. Upstream signatures are evidence of provenance; local hashes are what make the project update deterministic.

## SOURCE_DATE_EPOCH

When packaging or rebuilding release outputs, use `SOURCE_DATE_EPOCH` from the release commit time. Do not use the dependency release time as the app release time unless the packaging step explicitly requires it.

## Runtime Autoinstall

This project also has a runtime autoinstall path. If it downloads the latest upstream release at first run, that behavior is convenient but not reproducible. A clean dependency update PR should explicitly state one of these:

- runtime autoinstall remains a convenience path outside the release artifact reproducibility claim,
- runtime autoinstall is being changed to pinned version/hash behavior,
- release artifacts bundle the reviewed dependency binaries and runtime autoinstall is bypassed for packaged releases.

For a high-integrity default, prefer pinned/hash-verified runtime downloads for XMRig and P2Pool, matching the care already used for Tor.

## Validation

Run what exists locally:

- `go test ./...`
- `make verify-repro`
- dependency download scripts with checksum verification
- release/package build smoke tests when practical

If `make verify-repro` only proves local determinism, say that plainly. Do not overclaim cross-host reproducibility when native libraries or cgo are involved.

