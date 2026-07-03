# Reproducible builds

kind-miner aims for [reproducible builds](https://reproducible-builds.org): anyone
should be able to rebuild a published binary from source and get **the same bytes**.

This matters more here than for most apps. kind-miner holds your wallet address
and routes mining traffic over Tor — you are asked to trust the binary. Signed
checksums (see `dMartian.pub`) prove *we* built a given file; reproducibility lets
*you* prove that file is what the public source compiles to, with nothing added.

## Reproduce a release

```sh
git clone https://codeberg.org/dMartian/kind-miner
cd kind-miner
git checkout v0.1.0            # the exact tag you are verifying

make reproduce                 # or: scripts/reproduce.sh v0.1.0
sha256sum kind-miner           # compare against the published SHA256SUMS
```

A matching SHA256 means your binary is byte-for-byte identical to the release.

## Verify determinism locally

`verify-repro` builds twice with the toolchain you already have and compares the
hashes — a fast, offline check that the build is deterministic on one machine:

```sh
make verify-repro
# build A: <sha256>
# build B: <sha256>
# ✓ reproducible — identical bytes
```

## What is controlled

`scripts/reproduce.sh` removes the usual sources of nondeterminism:

| Input | How it's pinned |
|---|---|
| Go toolchain | `GOTOOLCHAIN=go1.25.0` in `scripts/reproduce.sh`, matching the `go` directive in `go.mod` that CI's `setup-go` installs. A **stock** release, never a locally patched `go`. |
| Filesystem paths | `-trimpath` — no `/home/you/...` in the binary. |
| Git state | `-buildvcs=false` — the working-tree "dirty" flag never leaks in. |
| Build id | `-ldflags -buildid=` — drops Go's nondeterministic build id. |
| Local `go env` | `GOENV=off` — ignores `~/.config/go/env`, so a personal `CGO_LDFLAGS` shim can't bake a host path into the build. |
| cgo flags | `CGO_*` cleared — the C/OpenGL libraries are found via standard system paths. |
| Dependencies | `go.sum` pins every module by hash. |
| Timestamps | `SOURCE_DATE_EPOCH` (the commit time) for any packaging step. |

## Scope and caveats (honest status)

- **The binary is reproducible on a given OS + arch with the pinned stock Go and
  the standard GUI dev libraries installed.** Because the GUI uses **cgo**
  (Fyne/GLFW/OpenGL), byte-identical results across *different* hosts also require
  the same C toolchain and system headers. A fully hermetic, digest-pinned build
  container that guarantees this across machines is tracked for the CI/packaging
  phase — it is not in place yet.
- **Cross-compiling the GUI is not reproducible from a single host today.** Each
  OS/arch is built natively (see `.github/workflows/release.yml`). Headless
  (`CGO_ENABLED=0`) server builds cross-compile reproducibly.
- **Signing is intentionally outside reproducibility.** A macOS notarized `.app`
  or an Authenticode-signed `.exe` embeds a unique signature and can never be
  bit-identical. The plan: reproduce the **unsigned payload**, and verify the
  signature as a separate, detached layer.

## Build-flag parity

The same flags are used in three places and must stay in sync:
`Makefile` (`GO_BUILD_FLAGS`), `scripts/reproduce.sh`, and the CI build step in
`.github/workflows/release.yml`.
