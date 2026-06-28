# Language recipes

Concrete, copy-pasteable starting points per ecosystem. Always set the clock first:

```sh
export SOURCE_DATE_EPOCH=$(git log -1 --pretty=%ct)
export LC_ALL=C LANG=C TZ=UTC
umask 022
```

---

## Go

The happy path is excellent — Go 1.21+ is *perfectly reproducible* for pure-Go builds (source is the only relevant input).

**Pure Go (no cgo) — fully reproducible, cross-host:**
```sh
CGO_ENABLED=0 go build -trimpath -buildvcs=false \
  -ldflags "-s -w -buildid= -X main.version=$VERSION" -o app ./cmd/app
```
- `CGO_ENABLED=0` removes the host C toolchain as an input.
- `-trimpath` strips local paths.
- `-buildvcs=false` stops the working-tree VCS/dirty state leaking in.
- `-buildid=` drops the non-deterministic build id; `-s -w` strip debug (optional but common for release).
- Pin the toolchain: `GOTOOLCHAIN=go1.X.Y` (matching the `toolchain` directive in `go.mod`), and use a **stock** release. Deps are pinned by `go.sum`; `go mod verify` checks them.

**With cgo (GUI/native, like Fyne/OpenGL):** the binary is reproducible *on a given OS+arch with the pinned Go and the same C toolchain + system headers*. Cross-host requires pinning the C side too (a digest-pinned container or documented distro). Keep cgo flags out of the build (`GOENV=off`, clear `CGO_*`) so host library paths don't bake in. Cross-compiling a cgo GUI from one host is generally **not** reproducible — build each OS/arch natively. (This repo's `scripts/reproduce.sh` is exactly this pattern.)

**Derive a build date in Go** from `SOURCE_DATE_EPOCH` instead of `time.Now()`:
```go
ts := time.Now()
if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
    if n, err := strconv.ParseInt(s, 10, 64); err == nil { ts = time.Unix(n, 0).UTC() }
}
```

## Rust

```sh
export RUSTFLAGS="--remap-path-prefix=$PWD=. --remap-path-prefix=$CARGO_HOME=/cargo"
cargo build --release --locked
```
- `--remap-path-prefix` for build paths (also remap the registry/`CARGO_HOME`).
- `--locked` enforces `Cargo.lock` (pin it; commit it for binaries).
- Pin the toolchain with `rust-toolchain.toml` (`channel = "1.XX.Y"`).
- `*-sys` crates invoke a C compiler → same cgo-style caveat (pin the C toolchain).
- Newer Cargo supports trim-paths via profile (`[profile.release] trim-paths = "all"` / `Cargo.toml`); prefer it when available.

## C / C++ (Make / CMake / Meson)

Compiler flags (GCC ≥7, Clang ≥16 honor `SOURCE_DATE_EPOCH` for `__DATE__`/`__TIME__`):
```sh
export CFLAGS="-ffile-prefix-map=$PWD=. -Wdate-time"
export CXXFLAGS="$CFLAGS"
export LDFLAGS="-Wl,--build-id=none"   # or a content-hash build-id
```
- `-ffile-prefix-map` normalizes paths in debug info and macros.
- `-Wdate-time` warns on `__DATE__`/`__TIME__`/`__TIMESTAMP__` usage.
- Avoid `-march=native` (host-CPU dependent) — pick an explicit baseline.
- Deterministic archives: modern `ar` is deterministic by default; otherwise `ar rD` / configure binutils `--enable-deterministic-archives`.

**CMake (≥3.8 for `SOURCE_DATE_EPOCH`, use `UTC`):**
```cmake
string(TIMESTAMP BUILD_DATE "%Y-%m-%d" UTC)
set(CMAKE_BUILD_WITH_INSTALL_RPATH ON)   # or CMAKE_SKIP_RPATH / CMAKE_BUILD_RPATH_USE_ORIGIN
```
Pass `-ffile-prefix-map` via `CMAKE_C_FLAGS`/`CMAKE_CXX_FLAGS`. RPATH options prevent the build dir leaking into binaries (catalog §2).

**Qt:** `rcc` embeds mtimes → `--format-version 1` (Qt ≥5.9) or `QT_RCC_SOURCE_DATE_OVERRIDE`/`SOURCE_DATE_EPOCH`. `moc` can hardcode build→source relative paths → use `-p`.

## Python

- **Wheels/sdists:** `wheel`/`setuptools` honor `SOURCE_DATE_EPOCH` for the zip member timestamps (clamped to ≥ 1980). Build with a pinned `build`/`setuptools`/`wheel` and a hashed lock:
  ```sh
  pip install --require-hashes -r requirements.txt
  python -m build
  ```
- **Bytecode (`.pyc`):** use hash-based pycs for reproducibility (`python -m compileall --invalidation-mode checked-hash`), or rely on `SOURCE_DATE_EPOCH` for timestamp-based ones.
- **Build-time codegen:** set `PYTHONHASHSEED=0` so set/dict ordering in generated output is stable.
- Pin everything with hashes (`pip-compile --generate-hashes`); `pip install` resolves floating versions otherwise (catalog §8).

## Node / JavaScript

- Install from lockfile only: `npm ci` (not `npm install`), or `pnpm install --frozen-lockfile` / `yarn --immutable`.
- `npm pack` tarballs embed mtimes and can vary in ordering → normalize with `strip-nondeterminism` on the `.tgz`, or pack via a tool that sorts and sets `SOURCE_DATE_EPOCH`.
- Bundlers (webpack/rollup/esbuild) can embed timestamps/hashes/build paths — disable build banners, pin the bundler version, and avoid content hashes seeded by absolute paths.
- Pin Node itself (`.nvmrc`/`engines`) and pin transitive deps in the lockfile.

## JVM (Maven / Gradle / sbt)

`javac` bytecode is reproducible; the `.jar` packaging is where timestamps/ordering creep in.

- **Maven:** set `<project.build.outputTimestamp>${env.SOURCE_DATE_EPOCH}</project.build.outputTimestamp>`; use the reproducible-build guidance and the `artifact:check-buildplan`/Reproducible Central `.buildspec` model to verify.
- **Gradle:** `tasks.withType(AbstractArchiveTask) { isPreserveFileTimestamps = false; isReproducibleFileOrder = true }` (Gradle ≥3.4).
- **sbt:** `sbt-reproducible-builds` plugin.
- Pin the JDK vendor+version; encoding UTF-8 (Java ≥18 defaults to it).

## OCI containers (Docker / Podman / Buildah)

Containers multiply non-determinism (apt mirrors, timestamps, layer ordering).

- **Pin the base image by digest:** `FROM debian:bookworm@sha256:…` — never a floating tag.
- **Pin packages:** `apt-get install -y --no-install-recommends pkg=exact.version`; clean caches in the same layer.
- **Timestamps:** BuildKit ≥0.10 propagates `SOURCE_DATE_EPOCH`; rewrite layer mtimes with `docker buildx build --build-arg SOURCE_DATE_EPOCH=$SOURCE_DATE_EPOCH --output type=image,name=…,rewrite-timestamp=true`.
- **Determinism of the app inside:** still apply the language recipe above; the container only fixes the *environment*, not the build.
- Strongest form: build the app with a hermetic toolchain (Guix/Nix) and use the container purely to pin system libs.

---

## Where flags must stay in sync (build-flag parity)

If the same build flags appear in more than one place — `Makefile`, a `reproduce.sh`, and a CI workflow — they **will** drift and silently break reproducibility. During review, treat any change to one copy without the others as a defect. The long-term fix is a single source of truth (one script the Makefile and CI both call), which this repo does via `scripts/reproduce.sh`.
