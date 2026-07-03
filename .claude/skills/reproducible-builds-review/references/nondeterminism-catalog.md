# Non-determinism catalog

The complete list of things that make builds differ, and how to fix each. In Mode A, diffoscope tells you *where* bytes differ; this tells you *why*. Source: reproducible-builds.org "Managing variance," distilled.

Almost every reproducibility bug is one of these eight classes.

---

## 1. Timestamps

The most common cause. The build embeds "now" — into binaries, archive member mtimes, generated headers, gzip headers, PDFs, manpages, `__DATE__`/`__TIME__` in C.

- **Fix:** set `SOURCE_DATE_EPOCH=$(git log -1 --pretty=%ct)` and make every timestamped step read it (see `language-recipes.md`).
- **Archives:** `tar --mtime=@$SOURCE_DATE_EPOCH …`; `gzip -n` (drop name+mtime); `zip`/jars need timestamp normalization (`strip-nondeterminism`, or set zip entry times).
- **C/C++:** `__DATE__`/`__TIME__` are non-deterministic by construction. Replace with a value derived from `SOURCE_DATE_EPOCH`; GCC ≥7 / Clang ≥16 already feed `SOURCE_DATE_EPOCH` into them and warn.
- **Smell:** `date`, `date +%s`, `$(date …)`, `time.Now()`/`Date.now()`/`datetime.now()` used in a *build* step or baked via `-X …date=$(date)` ldflags.

## 2. Build paths

The absolute path of the build directory leaks into binaries (debug info, `__FILE__`, assertions, RPATHs) and into archives.

- **Go:** `-trimpath`.
- **GCC/Clang:** `-ffile-prefix-map=$(pwd)=.` (covers `-fdebug-prefix-map` + `-fmacro-prefix-map`). Older compilers: `-fdebug-prefix-map=$(pwd)=.`.
- **Rust:** `RUSTFLAGS="--remap-path-prefix=$(pwd)=."` (and remap `$CARGO_HOME`/registry paths).
- **Cross-tool spec:** `BUILD_PATH_PREFIX_MAP`.
- **CMake RPATH:** `-DCMAKE_SKIP_RPATH=ON`, or `-DCMAKE_BUILD_WITH_INSTALL_RPATH=ON`, or `-DCMAKE_BUILD_RPATH_USE_ORIGIN=ON` (CMake ≥3.14).
- **Smell:** `/home/<user>/…`, `/build/…`, or CI workspace paths visible in `strings <binary>`.

## 3. File / input ordering

Filesystem readdir order is not stable across machines. Wildcards, `find`, `os.listdir`, `glob`, and "list all sources" steps produce different orders → different link order, different archive order, different generated tables.

- **Fix:** sort explicitly — `sort`, `sorted()`, `LC_ALL=C sort` (locale-stable), `find … | sort`, `tar --sort=name`.
- **Linkers/archivers:** pass inputs in a fixed order; `ar` deterministic mode (`ar rD`, or binutils built with `--enable-deterministic-archives`, the modern default).
- **Smell:** `$(wildcard …)`, `find` without `sort`, `glob.glob` results used unsorted, map/dict iteration emitted to output.

## 4. Locale / timezone / encoding

Sorting, number/date formatting, and case-folding depend on `LC_*`, `LANG`, and `TZ`. Default encoding affects text output.

- **Fix:** pin `LC_ALL=C LANG=C TZ=UTC` for the build; prefer UTF-8 explicitly; use UTC date/time APIs (`getUTC*`, `gmtime`, `datetime.timezone.utc`, CMake `STRING(TIMESTAMP … UTC)`).
- **Smell:** locale-sensitive `sort`, `strftime`/`date` without UTC, collation-dependent output.

## 5. Randomness / non-stable identifiers

PRNGs seeded from entropy, UUIDs, temp filenames embedded in output, hash-map iteration order (Python `PYTHONHASHSEED`, Go map ranges), Go/linker build-ids, ASLR-style cookies in generated code.

- **Fix:** seed deterministically or eliminate; sort hash-map output before emitting; `PYTHONHASHSEED=0` for build-time codegen; clear build-ids (`-ldflags -buildid=` in Go; `-Wl,--build-id=none` or a content-based build-id for ELF).
- **Smell:** `$RANDOM`, `uuidgen`, `mktemp` paths captured into artifacts, `rand()`/`secrets`/`crypto.randomBytes` at build time.

## 6. Embedded environment (host / user / build metadata)

Hostname, username, `$PWD`, CPU count, env vars, git "dirty" state, build counter, or compiler banner baked into the artifact.

- **Fix:** don't capture host/user; derive version from the **tag/commit**, not `git describe --dirty` on a mutable tree (Go: `-buildvcs=false` to stop embedding VCS state); avoid `-march=native` (host-CPU-dependent codegen — pick an explicit baseline like `-march=x86-64-v2`).
- **Smell:** `hostname`, `whoami`, `$USER`, `uname -n`, `nproc` feeding output; `-march=native`; `git describe --dirty` used as the embedded version.

## 7. Archive & packaging metadata

Even with deterministic *contents*, the container differs: member order, mtimes, uid/gid/owner names, permissions, compression level/headers, and tool-version banners.

- **Fix:** `tar --sort=name --mtime=@$SOURCE_DATE_EPOCH --owner=0 --group=0 --numeric-owner --pax-option=exthdr.name=%d/PaxHeaders/%f,delete=atime,delete=ctime`; `gzip -n`; normalize zip/jar with `strip-nondeterminism`; pin `umask 022` so permissions don't vary.
- **Containers:** see `language-recipes.md` (digest-pin base, `SOURCE_DATE_EPOCH`, BuildKit `rewrite-timestamp`).
- **Smell:** `tar`/`zip`/`gzip`/`npm pack`/`jar` invoked without normalization; default `umask` assumed.

## 8. Unpinned / volatile inputs

The build pulls something that can change underneath it: `latest` dependency, floating base image, network-fetched asset without a hash, a system library whose version varies, the compiler version itself.

- **Fix:** pin every input by content hash — lockfiles (`go.sum`, `Cargo.lock`, `package-lock.json`, hashed `requirements.txt`), vendored sources, container base by `@sha256:…`, downloaded assets verified against a recorded SHA256, and a pinned **stock** toolchain version.
- **Smell:** `:latest` tags, `go get`/`pip install` without pins at build time, `curl … | sh`, version ranges (`^`, `~`, `>=`) resolved at build, downloads with no checksum step.

---

## Diagnosis workflow

1. `diffoscope build-a build-b` (or the two release artifacts). Read *where* it differs.
2. Map the difference to a class above:
   - differing dates/mtimes → **§1 / §7**
   - paths in debug info/strings → **§2**
   - reordered symbol tables / file lists → **§3**
   - sort-order or formatting differences → **§4**
   - random-looking ids → **§5**
   - hostnames/usernames/dirty flags → **§6**
   - tar/zip headers → **§7**
   - "this dep/base image changed" → **§8**
3. Apply the fix, rebuild, re-diff. Repeat until diffoscope is empty.
4. Then escalate from local determinism to cross-environment with `reprotest` (it deliberately varies path/time/locale/user/umask/hostname — exercising §2/§1/§4/§6 at once).
