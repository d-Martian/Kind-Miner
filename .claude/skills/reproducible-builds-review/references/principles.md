# Principles

Source: <https://reproducible-builds.org/docs/>. Read this before making any reproducibility *claim*; the wording matters.

## Definition

> A build is **reproducible** if, given the same source code, build environment, and build instructions, any party can recreate **bit-for-bit identical** copies of all specified artifacts.

Three deliberate parts:

- **The same source** — pinned, e.g. an exact tag/commit.
- **The same build environment** — toolchain, libraries, OS, build path. Reproducibility is a property of *source + environment*, not source alone.
- **A defined set of artifacts** — the project says *which* outputs the claim covers (often "the unsigned binaries/archives"), and what is explicitly out of scope.

If a project can't point at all three, it doesn't yet have a reproducibility *claim* — it has an aspiration. The first deliverable is often just writing that scope down.

## Why it matters (the one-line rationales)

- **Verifiability / supply-chain integrity** — users can confirm a binary is the published source with nothing added. This is the headline reason for security-sensitive FOSS (wallets, messengers, miners, OS packages).
- **Detect compromise** — a tampered build server or a malicious mirror is caught when an independent rebuild disagrees.
- **Caching & speed** — deterministic outputs are content-addressable; identical inputs need not be rebuilt (Nix, Bazel, ccache rely on this).
- **Debuggability** — "works on my machine" shrinks when the build can't vary underneath you.

## The reproducibility ladder

State which rung you are on. Each is a real, useful level; conflating them is the most common honesty failure.

1. **Locally deterministic** — building twice *on the same machine* yields identical bytes. Necessary, not sufficient. Catches embedded timestamps, build-ids, unsorted output. This is what a `build-twice-and-compare` check proves.
2. **Cross-environment reproducible** — different path, time, locale, user, umask, hostname produce identical bytes. This is what `reprotest` and a second independent builder prove. This is the level most people *mean* by "reproducible."
3. **Cross-toolchain / fully specified** — identical bytes given the *documented* toolchain and pinned dependencies on any host. Requires pinning the compiler and (for native code) the C toolchain and system headers.
4. **Bootstrappable** — the toolchain itself is built from a small, audited seed rather than a trusted binary blob (GNU Guix, Bitcoin Core, the Go toolchain's self-verification). The strongest guarantee; rarely required for an application, but the gold standard the ecosystem aims at.

A typical good-FOSS-app target is rung 2–3 for the release artifacts, with caveats documented for anything (cgo, signing) that can't reach it.

## Define & distribute the environment

Because the environment is an input, you must pin and share it:

- **Toolchain** — pin the exact compiler/SDK version, and use a **stock, unmodified** release (a locally patched compiler can't be reproduced by others). For Go, `GOTOOLCHAIN=go1.X.Y`; for Rust, a pinned `rust-toolchain.toml`; for C, a named distro/container image.
- **Dependencies** — pin by content hash, not by version range: `go.sum`, `Cargo.lock`, `package-lock.json`/`npm ci`, hashed `requirements.txt`, vendored sources. A version number is not a hash.
- **Build path** — either fix it (build at a known path) or make it irrelevant (`-trimpath`, `-ffile-prefix-map`, `BUILD_PATH_PREFIX_MAP`). The second is strictly better.
- **Distribution of the environment** — the strongest forms hand others a way to *recreate* it: a digest-pinned container image, a Guix/Nix manifest, or a documented "install exactly these versions." A `Dockerfile` whose base image is pinned by `@sha256:…` is the pragmatic sweet spot for most projects.

## SOURCE_DATE_EPOCH (the one variable you almost always need)

The cross-tool standard for "the moment to pretend it is now." Set it from the source, not the wall clock:

```sh
export SOURCE_DATE_EPOCH=$(git log -1 --pretty=%ct)   # last commit time
```

Consumed natively by GCC ≥7, Clang ≥16, CMake ≥3.8 (with `UTC`), Meson, gzip/tar (via `--mtime=@$SOURCE_DATE_EPOCH`), Sphinx, Doxygen, RPM, dpkg/debhelper, and Docker Buildx ≥0.10. For languages/tools that ignore it, read it in your own build code (per-language snippets in `language-recipes.md`). `BUILD_PATH_PREFIX_MAP` is the analogous spec for normalizing build paths.

Rule: use the **source's** timestamp (commit/tag/changelog), never a dependency's release time or the current time.

## Honest scope statement (the deliverable that prevents overclaiming)

Every reproducible-build claim should ship a short statement answering:

- **What** is reproducible (which artifacts).
- **How** to reproduce it (one copy-pasteable command + how to compare).
- **What is controlled** (toolchain, paths, deps, timestamps) — a table is ideal.
- **Caveats** — what is *not* reproducible and why (cross-host cgo, cross-compiled GUI, signed installers), and what's still aspirational.

This repo's `REPRODUCIBLE.md` is a good worked example of the table + caveats form. The template in `assets/templates/REPRODUCIBLE.md.tmpl` generalizes it.

The cardinal sin is implying rung 3–4 while having only verified rung 1. Underclaiming is fine; overclaiming destroys the trust the whole exercise is meant to create.
