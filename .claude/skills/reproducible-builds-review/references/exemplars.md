# Exemplars — how the best projects do it

Study these to copy *patterns*, not ceremony. Each row is "the transferable idea."

## Go toolchain — "perfectly reproducible," source is the only input

Go 1.21+ made its own toolchain bit-for-bit reproducible regardless of host OS/arch/C-toolchain/build-dir, and ships `gorebuild` to verify published binaries against source. They got there by eliminating each input one at a time: read time from a `VERSION` file (not the clock), drop cgo from the toolchain build (`CGO_ENABLED=0`), `-trimpath` for the build dir, sort all outputs, clear archive uid/gid/mtimes.

- **Transferable:** the method is a *checklist of inputs to eliminate* (exactly the catalog). For your own Go app, `CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags=-buildid=` gets you cross-host reproducibility for free.
- Ref: <https://go.dev/blog/rebuild>

## Bitcoin Core — Guix + multi-signer attestation

Independent contributors build each release in **GNU Guix** (a fully specified, bootstrappable environment), confirm identical hashes, and publish detached signatures to a separate `guix.sigs` repo. Three separated phases: `guix-build` → `guix-codesign` → `guix-attest`. `SOURCE_DATE_EPOCH` defaults to the commit time.

- **Transferable, in miniature:** (1) separate *building* from *signing* — only authorized keys sign, but anyone can build and attest; (2) put attestations in a public repo so trust is distributed, not vested in one CI server; (3) treat signing as a detached layer over a reproducible unsigned payload.
- Ref: `bitcoin/contrib/guix/README.md`

## Tor Project / Tails — reproducibility as a security requirement

Anonymity tools were early adopters precisely because users *must* trust the binary. They build in fully specified container/VM environments and publish the recipe so others can rebuild.

- **Transferable:** when the threat model is "is this binary backdoored?", reproducibility isn't a nicety — it's the control. Document the build environment as part of the security posture.
- Ref: <https://reproducible-builds.org/who/projects/>

## Debian + reprotest + rebuilderd — continuous, fleet-wide verification

Debian rebuilds the archive and tracks reproducibility status per package over time, using `reprotest` (vary the environment), `.buildinfo` (record the exact inputs), and `rebuilderd` (an independent rebuild service). Arch Linux runs `rebuilderd` similarly.

- **Transferable:** `reprotest` is the single best tool to adopt for an app — it turns "deterministic on my machine" into "survives path/time/locale/user/umask changes." `.buildinfo` is the idea of *recording the environment* so a rebuild years later is possible.
- Refs: <https://reproducible-builds.org/tools/>, <https://tests.reproducible-builds.org>

## JVM Reproducible Central — verify against published releases

Rebuilds Maven Central artifacts from a `.buildspec` and reports which are reproducible, driving the ecosystem fix-by-fix.

- **Transferable:** the `.buildspec` model — a small file recording exactly how to rebuild — plus public pass/fail pressure. Even a single project benefits from a committed "here's the exact build" record.

---

## This repo as a worked Go+cgo example

kind-miner is a useful study because it's an *honest, partial* case — a Go GUI that needs cgo (Fyne/GLFW/OpenGL), so it can't claim the free cross-host reproducibility a pure-Go app gets. What it does well, and what to imitate:

- **Single source of truth for flags.** `scripts/reproduce.sh` is called by both `make reproduce` and `make verify-repro`; the Makefile's `GO_BUILD_FLAGS` mirrors it. (The remaining risk — CI having a *third* copy of the flags — is exactly the build-flag-parity trap in `plan-review.md`.)
- **Hermetic environment, not just flags.** `reproduce.sh` pins a **stock** `GOTOOLCHAIN=go1.X.Y`, sets `GOENV=off` (so a personal `~/.config/go/env` can't bake a host path in), clears `CGO_*`, and fixes `LC_ALL/LANG/TZ` + `umask`. That's catalog §2/§4/§6/§8 closed in one script.
- **Two-tier verification.** `verify-repro` builds twice with the *local* toolchain for a fast offline determinism check (rung 1); `reproduce` uses the *pinned* toolchain for the real claim (rung 3 on a matching host).
- **An honest `REPRODUCIBLE.md`.** It states exactly what's controlled (a table), and the caveats: cgo means cross-host needs the same C toolchain; cross-compiling the GUI from one host isn't reproducible; signing is intentionally out of scope (reproduce the unsigned payload). This is the model honesty statement — copy its structure.

The next rung for a project like this is a **digest-pinned build container** that also pins the C toolchain + system headers, turning the "reproducible on a matching host" caveat into "reproducible, full stop" — and adding a CI repro job (templates in `assets/templates/`) so it stays that way.
