# Reproducible builds

kind-miner aims for [reproducible builds](https://reproducible-builds.org): anyone
should be able to rebuild a published binary from source and get **the same bytes**.

This matters more here than for most apps. kind-miner holds your wallet address
and routes mining traffic over Tor — you are asked to trust the binary. Signed
checksums (see `dMartian.pub`) prove *we* built a given file; reproducibility lets
*you* prove that file is what the public source compiles to, with nothing added.

## Reproduce a release

On an x86_64 Linux machine with podman, one command rebuilds everything a
container can reproduce and compares it with what the release published:

```sh
git clone https://github.com/d-Martian/Kind-Miner && cd Kind-Miner
scripts/rebuild-release.sh v1.2.3          # add --sign to co-sign, --upload to publish it
```

It checks out the tag and runs the release's own recipes — the linux-amd64 GUI
archive (`scripts/release-gui-linux.sh`), the amd64 `.deb`
(`scripts/release-deb.sh`, with xmrig from `scripts/build-xmrig.sh`), the
desktop `.deb` (`scripts/release-deb-desktop.sh`, with the engines
`tools/stage-engines` stages from the same pins; this one needs Go on the
host), and the engine source tarballs (`scripts/source-tarballs.sh`) — then prints, file by
file, whether it got the same bytes. The lines it reproduced go in
`SHA256SUMS.rebuilt-<host>`; `--sign` signs that with the rebuilder's own
minisign key (its public half is in `rebuilders/<host>.pub`), which is a second
machine, not GitHub's, vouching for those bytes.

## A second rebuilder

Release artifacts are built on GitHub's runners. A rebuilder is a machine
that is not GitHub's — the maintainer's Fedora box, to start — that runs
`scripts/rebuild-release.sh` on every release and, when everything it rebuilt
matched, uploads `SHA256SUMS.rebuilt-<host>` and its `.minisig` to the release.
Anyone can check the co-signature with `minisign -V -p rebuilders/<host>.pub`.
It covers what an x86_64 machine can rebuild; the macOS and Windows builds,
the AppImage, the Flatpak and the arm64 `.deb` are outside it (see Scope).

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
| Module source | `GOFLAGS=-mod=mod` — the module cache, checked by `go.sum`, never a stray (gitignored) `vendor/` tree, which would change the binary. |
| C toolchain, headers, packaging tools | the pinned build container (below), for the linux GUI and the `.deb`s. |
| Dependencies | `go.sum` pins every module by hash. |
| Timestamps | `SOURCE_DATE_EPOCH` (the commit time) for any packaging step. |

## Scope and caveats (honest status)

- **The linux GUI and the `.deb`s are reproducible across hosts**, because they
  are built in `packaging/buildenv/Containerfile`: Debian 12 by index digest,
  every package from snapshot.debian.org at one fixed moment, and Go 1.25.0 by
  its published SHA256. `scripts/in-build-container.sh` runs builds in it with
  the source read-only and the network off (Go modules are fetched first,
  checked by `go.sum`). The GUI uses **cgo** (Fyne/GLFW/OpenGL), so outside
  that container its bytes still depend on the host's C toolchain and headers.
- **macOS and Windows builds** are native and not reproducible across hosts;
  **the AppImage and the Flatpak** are packaged with tools whose output is not
  yet pinned. A rebuilder skips all of these.
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
`Makefile` (`GO_BUILD_FLAGS`), `scripts/reproduce.sh` (which the container
recipes call), and the macOS/Windows CI build step in
`.github/workflows/release.yml`.

Archive flags have a single source of truth: `scripts/package.sh`. The Makefile
`bundle-*` targets, `scripts/vendor-tarball.sh`, and the CI packaging step all
call it, so a release tarball built locally and one built by CI are byte-identical.

## CI

All CI runs on GitHub Actions:

| Workflow | Trigger | What it guards |
|---|---|---|
| `.github/workflows/repro-verify.yml` | push, PR, manual | Builds the linux release archive twice in the pinned container, from two different paths, and fails if the SHA256 differ |
| `.github/workflows/release.yml` | `v*` tag | The linux GUI and the `.deb`s in the pinned container, native macOS/Windows builds, deterministic archives, uploaded to a draft release |
| `.github/workflows/kind-minerd.yml` | push, PR, manual | kind-minerd has no Fyne dependency, builds with `CGO_ENABLED=0`, and rebuilds bit-for-bit for amd64 and arm64 |
| `.github/workflows/xmrig-build.yml` | changes to the xmrig recipe or pins, manual | Builds the patched xmrig twice on native x86_64 and aarch64 runners, fails if the SHA256 differ, and checks the donation patch took |
| `.github/workflows/dependency-watch.yml` | weekly cron, manual | Opens a tracking issue when XMRig/P2Pool publish a new release |
