# Plan / diff review (Mode B)

Reviewing a development plan, PR, or working-tree diff for reproducibility regressions — so bug fixes and features don't quietly break a property that took effort to establish. The bar is *proportionate*: flag real risks, don't bikeshed an unrelated one-line fix.

## What to actually look at

Most code changes have **zero** reproducibility impact. Focus on changes that touch:

- **Build flags / build scripts** — `Makefile`, `*.sh`, `build.rs`, `setup.py`, `CMakeLists.txt`, `*.gradle`.
- **Dependencies** — `go.mod`/`go.sum`, `Cargo.toml`/`Cargo.lock`, `package.json`/lockfile, `requirements*.txt`, vendored trees, container base images.
- **Code that runs at build time** — codegen, `go:generate`, macros, `build.rs`, asset embedding, bundlers.
- **Packaging** — archive/installer/image creation, release workflows.
- **Toolchain / CI** — compiler version, runner image, the build step itself.

If the diff touches none of these, the verdict is usually "no reproducibility impact."

## Red-flag checklist

Map each to the catalog (`nondeterminism-catalog.md`) and the fix in `language-recipes.md`.

**Timestamps (§1)**
- [ ] New `time.Now()`/`Date.now()`/`datetime.now()`/`__DATE__`/`__TIME__` whose value reaches an artifact.
- [ ] `date`/`$(date)` in a build script feeding a version, banner, or filename.
- [ ] New archive/zip/image creation that doesn't set `SOURCE_DATE_EPOCH`-derived mtimes.

**Inputs / pinning (§8)**
- [ ] A new dependency added without updating the lockfile, or a range (`^`, `~`, `>=`, `latest`) instead of a pin.
- [ ] A build-time `curl`/`wget`/`go get`/`pip install`/`npm install` without a hash or lockfile.
- [ ] A container base image referenced by tag instead of `@sha256:` digest.
- [ ] A toolchain bump (Go/Rust/JDK/compiler) — reproducibility is relative to a toolchain, so this *changes the published hash*. Legitimate, but the pinned version and the docs/`REPRODUCIBLE.md` must move together.

**Paths / host leakage (§2/§6)**
- [ ] Dropped `-trimpath` / `-ffile-prefix-map` / `--remap-path-prefix`, or a new compile path not covered by them.
- [ ] New `-march=native`, `hostname`, `whoami`, `$USER`, `nproc`, or `git describe --dirty` reaching an artifact.

**Ordering / randomness (§3/§5)**
- [ ] New `find`/`wildcard`/`glob`/`os.listdir` whose result feeds output without an explicit `sort`.
- [ ] Map/dict/set iteration emitted to a generated file.
- [ ] New `uuidgen`/`$RANDOM`/`mktemp`/unseeded RNG whose value lands in an artifact.

**Packaging (§7)**
- [ ] New `tar`/`zip`/`jar`/`npm pack` without `--sort`/normalized mtimes/`strip-nondeterminism`.

**Structural / drift**
- [ ] Build flags edited in **one** of several places they're duplicated (Makefile vs reproduce script vs CI). Demand parity, or push for a single source of truth.
- [ ] A reproducibility check removed, disabled, or made non-blocking in CI.
- [ ] The scope/caveats in `REPRODUCIBLE.md` no longer match reality after the change.

## How to give the verdict

Recommend in prose, sized to the risk:

- **No reproducibility impact** — change doesn't touch the build surface. Say it in one line and move on.
- **Impact, with required fixes** — name each flagged item, the catalog class, and the concrete fix. Where cheap, ask for a `rebuild_compare.sh` before/after (or that the CI repro job stays green) as evidence.
- **Needs a verification run before merge** — the change *should* be reproducibility-neutral but you can't tell statically (new codegen, new packaging step, toolchain bump). Ask for a rebuild-and-compare and, for packaging/path changes, a `reprotest` pass.

For a toolchain bump specifically: it's not a "regression," but it *invalidates previously published hashes*. The review outcome is: bump the pinned version everywhere, regenerate/republish `SHA256SUMS` from the new toolchain, and note it in the changelog/`REPRODUCIBLE.md`.

## Tie-in with this repo's dependency flow

When the change is an XMRig/P2Pool dependency bump, the `monero-miner-dependency-updater` skill already encodes the pin-and-hash gates; this review is the reproducibility lens on top: confirm the new asset is pinned by SHA256, that release packaging doesn't fall back to a `latest` download, and that the runtime-autoinstall reproducibility caveat is still stated correctly.
