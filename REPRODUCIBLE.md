# Reproducible builds

kind-miner aims for [reproducible builds](https://reproducible-builds.org): anyone
should be able to rebuild a published binary from source and get **the same bytes**.

This matters more here than for most apps. kind-miner holds your wallet address
and routes mining traffic over Tor — you are asked to trust the binary. Signed
checksums (see `dMartian.pub`) prove *we* built a given file; reproducibility lets
*you* prove that file is what the public source compiles to, with nothing added.

## Reproduce a release

```sh
git clone https://github.com/kind-miner/kind-miner
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

## xmrig

kind-miner builds xmrig from source rather than trusting upstream's binary, for
two reasons: upstream's binary donates 1% over a direct connection even when
told not to, and upstream publishes no linux-arm64 build at all.

```sh
scripts/build-xmrig.sh          # → dist/xmrig/linux-<arch>/xmrig, prints its SHA256
```

| Input | How it's pinned |
|---|---|
| Sources | xmrig tag, libuv, hwloc, OpenSSL by SHA256 in `engines/xmrig/sources.lock`; checked before anything is built |
| Toolchain | `alpine:3.22` by index digest (covers amd64 and arm64), every package at an exact version (`engines/xmrig/Containerfile`) |
| Network | none during the compile (`podman run --network=none`) |
| Paths | fixed `/build` inside the container, plus `-ffile-prefix-map` |
| Timestamps | `SOURCE_DATE_EPOCH` = the xmrig tag's commit time, from the lock — the binary changes when xmrig or the recipe does, not with every kind-miner commit |
| Build id | `-Wl,--build-id=none` |
| Changes to upstream | exactly the two patches in `engines/xmrig/patches/` |

Caveats: each architecture is built on its own kind of machine, not
cross-compiled, so an x86_64 host reproduces the x86_64 binary and an aarch64
host the aarch64 one. Alpine removes superseded package versions from its
repositories; when a pin stops installing, the build fails and the toolchain has
to be re-pinned and the hashes re-published. libuv, hwloc and the xmrig tarball
are pinned on first download (OpenSSL matches upstream's published SHA256).

## kind-minerd

The headless daemon builds from the same script with `KM_TARGET=kind-minerd`,
which switches the package and sets `CGO_ENABLED=0`. With no C in the build it
cross-compiles, and reproduces, from any host:

```sh
make reproduce-minerd                                   # this host's arch
KM_TARGET=kind-minerd GOARCH=arm64 scripts/reproduce.sh # e.g. for a Nodo
```

`.github/workflows/kind-minerd.yml` builds it twice for amd64 and arm64 from
different paths and compares the hashes, and fails if it ever depends on Fyne
or stops building without cgo.

## Build-flag parity

The same compiler flags are used in three places and must stay in sync:
`Makefile` (`GO_BUILD_FLAGS`), `scripts/reproduce.sh`, and the CI build step in
`.github/workflows/release.yml`.

Archive flags have a single source of truth: `scripts/package.sh`. The Makefile
`bundle-*` targets, `scripts/vendor-tarball.sh`, and the CI packaging step all
call it, so a release tarball built locally and one built by CI are byte-identical.

## CI

All CI runs on GitHub Actions:

| Workflow | Trigger | What it guards |
|---|---|---|
| `.github/workflows/repro-verify.yml` | push, PR, manual | Rebuilds the binary from two different paths and fails if the SHA256 differ |
| `.github/workflows/release.yml` | `v*` tag | Native per-OS builds + deterministic archives, uploaded to a draft release |
| `.github/workflows/kind-minerd.yml` | push, PR, manual | kind-minerd has no Fyne dependency, builds with `CGO_ENABLED=0`, and rebuilds bit-for-bit for amd64 and arm64 |
| `.github/workflows/xmrig-build.yml` | changes to the xmrig recipe or pins, manual | Builds the patched xmrig twice on native x86_64 and aarch64 runners, fails if the SHA256 differ, and checks the donation patch took |
| `.github/workflows/dependency-watch.yml` | weekly cron, manual | Opens a tracking issue when XMRig/P2Pool publish a new release |
