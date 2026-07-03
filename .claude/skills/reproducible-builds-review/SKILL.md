---
name: reproducible-builds-review
description: Use this skill to review a project's build for reproducibility and make it reproducible (deterministic, bit-for-bit, suitable for FOSS), or to audit a development plan / PR / diff so that bug fixes and feature work do not regress reproducibility. Covers SOURCE_DATE_EPOCH, -trimpath/build flags, diffoscope, reprotest, lockfile/digest pinning, build-environment definition, and per-language recipes (Go, Rust, C/C++/CMake, Python, Node, JVM, containers). Primary reference: reproducible-builds.org.
---

# Reproducible Builds Review

A reproducible build lets anyone recompile the published source and get **the same bytes**. That is what turns "trust us, this binary is the source" into something a third party can independently verify. This skill has two jobs:

- **Mode A — Audit & remediate.** Inspect a project, find what makes its build non-deterministic, and fix it (or hand back a prioritized plan).
- **Mode B — Plan / diff review.** Review a proposed change (a development plan, a PR, a diff) and flag anything that would *regress* reproducibility before it lands.

Primary reference: <https://reproducible-builds.org>. Default to its definitions and the `SOURCE_DATE_EPOCH` / `BUILD_PATH_PREFIX_MAP` specs over ad-hoc advice.

## Core idea (keep this in mind for both modes)

> Ensure **stable inputs**, ensure **stable outputs**, and **capture as little as possible from the build environment**.

Every reproducibility bug is one of: a timestamp, a path, an ordering, a locale/timezone, an embedded host/user/build-id, an unpinned input, or unseeded randomness. The catalog in `references/nondeterminism-catalog.md` is the exhaustive list; almost everything maps to it.

Be honest about *which level* of reproducibility a project actually has — see the ladder in `references/principles.md`. Never claim cross-host bit-for-bit reproducibility when only local determinism was verified.

## First pass (read-only, both modes)

Run the inventory before recommending anything:

```sh
sh .claude/skills/reproducible-builds-review/scripts/audit_reproducibility.sh
```

It detects the ecosystems present, reports whether deterministic flags / `SOURCE_DATE_EPOCH` / lockfiles / verification infra exist, and greps for common non-determinism smells. It is read-only and makes no network calls.

Then read, if present: `REPRODUCIBLE.md`, `RELEASING.md`, `Makefile` / build scripts, CI workflows (`.woodpecker*`, `.forgejo/`, `.github/`), `Dockerfile`/`Containerfile`, and the release artifact list.

## Mode A — Audit & remediate

1. Run the first-pass inventory and read the build/release path.
2. **Establish a baseline.** Build the artifact twice and compare hashes. Use the project's own check if it has one (e.g. a `verify-repro` target); otherwise:
   ```sh
   sh .claude/skills/reproducible-builds-review/scripts/rebuild_compare.sh "<build command>" <artifact>
   ```
   If they already differ on one host, that is the first bug to chase — diffoscope the two outputs.
3. **Diagnose** each difference against `references/nondeterminism-catalog.md`. diffoscope tells you *where* bytes differ; the catalog tells you *why* and how to fix it.
4. **Apply the language-specific recipe** from `references/language-recipes.md` (build flags, path remapping, archive normalization, lockfile/digest pinning).
5. **Define the build environment.** Reproducibility is a property of *source + environment*. Pin the toolchain (stock release, version-pinned), pin dependencies by hash, and document the environment so others can recreate it. See `references/principles.md` ("Define & distribute the environment").
6. **Re-verify** with `rebuild_compare.sh`, and for cross-environment confidence run `reprotest` (varies path/time/locale/user/umask) — see `references/verification.md`.
7. **Wire verification into CI** so regressions are caught automatically. Templates live in `assets/templates/`.
8. **Document honest scope** in a `REPRODUCIBLE.md` (template: `assets/templates/REPRODUCIBLE.md.tmpl`): how to reproduce, what is controlled, and the explicit caveats (e.g. cgo/native deps, signing).

## Mode B — Plan / diff review

Use this when reviewing a development plan, a PR, or a working-tree diff for a bug fix or feature. The goal is to catch reproducibility regressions *before* they land, without blocking ordinary development.

1. Identify what the change touches: build flags, dependencies, code that runs *at build time*, packaging/archive creation, CI build steps, or the toolchain.
2. Walk the change against the red-flag checklist in `references/plan-review.md` (new timestamps, unpinned/`latest` inputs, embedded host/path/user, archive creation without sorted/normalized metadata, generated code with non-stable ordering, a second copy of build flags that can drift, etc.).
3. For any flagged item, recommend the concrete fix from the catalog/recipes, and — when practical — ask for a before/after `rebuild_compare.sh` (or CI repro job) as evidence.
4. Keep **build-flag parity**: if the same flags live in more than one place (Makefile, build script, CI), a change to one must update all. Drift here silently breaks reproducibility.
5. Output a short verdict: *no reproducibility impact* / *impact with required fixes* / *needs a verification run before merge*. Recommend in prose; don't gate trivial changes.

## References

Load only what the task needs:

- `references/principles.md` — definitions, the reproducibility ladder (local determinism → cross-host → bootstrappable), defining and distributing the build environment, and writing an honest scope statement.
- `references/nondeterminism-catalog.md` — the complete catalog of non-determinism sources and their fixes. The backbone of Mode A diagnosis.
- `references/language-recipes.md` — concrete, copy-pasteable recipes per ecosystem: Go, Rust, C/C++ (Make/CMake), Python, Node/JS, JVM (Maven/Gradle), and OCI containers.
- `references/verification.md` — diffoscope, reprotest, rebuild-and-compare, `SHA256SUMS`/`.buildinfo`, and the independent-rebuilder / multi-signer attestation model.
- `references/plan-review.md` — Mode B red-flag checklist and how to give a proportionate verdict.
- `references/exemplars.md` — how top projects do this well (Go's perfectly-reproducible toolchain, Bitcoin Core + Guix + guix.sigs, Tor, Debian/reprotest, Arch rebuilderd, JVM Reproducible Central) and the transferable patterns, plus this repo as a worked Go+cgo example.

## Scripts

- `scripts/audit_reproducibility.sh` — read-only inventory and non-determinism smell scan. No writes, no network.
- `scripts/rebuild_compare.sh "<build cmd>" <artifact>` — builds twice, compares SHA256, and runs diffoscope on a mismatch. Set `VARY=1` to perturb timezone/locale/umask on the second build to catch environment leakage.

## Templates

In `assets/templates/` — adapt names, commands, and versions before copying:

- `woodpecker-repro-verify.yml` / `forgejo-actions-repro-verify.yml` — a CI job that rebuilds and compares hashes (and can diffoscope on failure).
- `REPRODUCIBLE.md.tmpl` — a project-facing doc: how to reproduce, what is controlled, honest caveats.

## Hard gates (do not overclaim)

- Do **not** state a project "has reproducible builds" unless you (or its CI) actually rebuilt and got matching bytes. "Deterministic on one host" ≠ "reproducible across hosts." Say which you verified.
- If native code / cgo / a system C toolchain is involved, cross-host reproducibility requires pinning that toolchain too. Call it out explicitly rather than implying it works.
- Signing (Authenticode, macOS notarization, embedded GPG) makes outputs non-identical *by design*. Reproduce the **unsigned payload** and treat the signature as a separate, detached layer. Never present a signature mismatch as a reproducibility failure.
- A claim is only as strong as its weakest input: a pinned binary with an unverified hash, or a `latest`/floating dependency, breaks the chain regardless of build flags.
- Don't silently "fix" reproducibility by deleting useful build metadata the project relies on (e.g. an embedded version). Make it *deterministic* (derive from source/tag/`SOURCE_DATE_EPOCH`) instead of removing it.

## Verification before final answer

- State the level achieved: local determinism (built twice, same host) and/or cross-environment (reprotest / second host / CI) — with the actual hashes or the diffoscope result.
- Confirm inputs are pinned: toolchain version, dependency lockfile/hashes, and any container base image by digest.
- Confirm verification is reproducible by others: a documented build command and, ideally, a CI repro job.
- List the honest caveats (cgo/native, cross-compile, signing) and anything still unverified.
