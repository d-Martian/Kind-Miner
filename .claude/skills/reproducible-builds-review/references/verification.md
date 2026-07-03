# Verification

How to *prove* reproducibility rather than assert it. Tools from <https://reproducible-builds.org/tools/>.

## The minimum: rebuild and compare (local determinism, rung 1)

Build the artifact twice and compare hashes. This catches embedded timestamps, build-ids, and unsorted output — the cheap, offline check.

```sh
sh .claude/skills/reproducible-builds-review/scripts/rebuild_compare.sh "<build command>" <artifact>
```

Or by hand:
```sh
<build command>; sha256sum artifact > a.txt
<clean>; <build command>; sha256sum artifact > b.txt
diff a.txt b.txt && echo "identical" || diffoscope <artifact-a> <artifact-b>
```

A project should expose this as a target (e.g. `make verify-repro`). It is *necessary but not sufficient* — same host, same time-ish, same path.

## diffoscope — locate the difference

When two builds differ, diffoscope tells you *where*, by recursively unpacking archives and rendering binaries human-readable.

```sh
diffoscope build-a/app build-b/app                 # terminal
diffoscope --html report.html build-a.tar build-b.tar
diffoscope --text - a.whl b.whl                    # plain text to stdout
```
Install: `apt/dnf/pacman install diffoscope`, `pip install diffoscope`, `brew install diffoscope`, or the container at `registry.salsa.debian.org/reproducible-builds/diffoscope`. Handles ELF, tar/zip/jar/rpm/apk, PDFs, images, SQLite, WebAssembly, 100+ formats. Map each reported difference to a class in `nondeterminism-catalog.md`.

## reprotest — prove cross-environment reproducibility (rung 2)

reprotest builds **twice in deliberately different environments** and diffoscopes the results. This is the real test of "reproducible," not just "deterministic."

```sh
reprotest 'CGO_ENABLED=0 go build -trimpath -o app ./cmd/app' 'app'
reprotest --vary=+all '<build command>' '<artifact>'
```
It varies: `build_path`, `time`/`timezone`, `locale`, `user_group`, `umask`, `home`, `kernel`, `fileordering` (via `disorderfs`), `exec_path`, etc. A pass here means the artifact survives path/time/locale/user/umask changes — exercising catalog §1/§2/§4/§6/§7 at once. Install: `apt install reprotest` (or pip).

Helper tools reprotest leans on: **disorderfs** (randomizes readdir order to flush out §3) and **strip-nondeterminism** (post-normalizes zip/jar/gzip/png metadata — §7).

## Independent rebuild + attestation (rung 2–3, the FOSS gold standard)

The strongest verification is *someone else's machine* getting your bytes. Two established models:

- **Multi-signer attestations (Bitcoin Core / Guix).** Multiple independent builders run the same pinned (Guix) build, confirm identical hashes, and publish signatures to a separate repo (`guix.sigs`). Trust is distributed; no single build server is authoritative. Adoptable in miniature: publish your `SHA256SUMS`, invite a second maintainer to rebuild and co-sign, record both signatures.
- **Continuous independent rebuilders (Debian / Arch `rebuilderd`).** A service monitors releases, rebuilds from source, and reports match/mismatch over time, producing `.buildinfo`-style records of the exact environment used.

For a small project, the pragmatic ladder is: (1) CI rebuilds and compares on every release; (2) publish a documented `make reproduce` + `SHA256SUMS`; (3) get one independent co-signer.

## Recording the build for others — `SHA256SUMS` and `.buildinfo`

- **`SHA256SUMS`** (optionally GPG-signed `.asc`) — the published hashes a verifier compares against. The signature proves *who* built it; reproducibility proves *what* it is. (See the `codeberg-release` skill for the signing flow.)
- **`.buildinfo` / `.buildspec`** — a record of the exact inputs (toolchain versions, dependency hashes, environment) needed to recreate the build. Debian's `.buildinfo` and JVM Reproducible Central's `.buildspec` are the canonical forms. Even a hand-written "build environment" section in `REPRODUCIBLE.md` serves this purpose.

## CI as the always-on verifier

Wire rebuild-and-compare into CI so regressions fail the build, not the release. Templates: `assets/templates/woodpecker-repro-verify.yml`, `assets/templates/forgejo-actions-repro-verify.yml`. For real cross-environment coverage, run the second build with varied `TZ`/`LC_ALL`/build path (or a `reprotest` job) rather than two identical runners.

## What "verified" honestly means

Report the *rung you actually reached*:

- "Built twice on this host, identical SHA256" → rung 1 (local determinism). Say so; don't imply more.
- "reprotest `--vary=+all` passes" or "a second machine reproduced the published hash" → rung 2.
- "Reproduced with the documented pinned toolchain on host X and Y" → rung 3.

Always pair the claim with the evidence (the hash, the diffoscope-clean result, or the rebuilder report).
