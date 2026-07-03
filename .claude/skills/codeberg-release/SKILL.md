---
name: codeberg-release
description: Use this skill when preparing, verifying, signing, or publishing a Codeberg/Forgejo release with version tags, release binaries, reproducibility checks, SHA256 hashes, GPG signatures, release notes, and release assets.
---

# Codeberg Release

Use this skill to create high-integrity Codeberg releases. Default to a local signing flow so the user's private GPG key stays on their machine. Publish through Codeberg/Forgejo only after the user explicitly wants the release created or uploaded.

## Release Flow

1. Inspect the repository:
   - `git remote -v`
   - `git status --short`
   - `git tag --sort=-v:refname | head`
   - release docs such as `REPRODUCIBLE.md`, `RELEASING.md`, `CHANGELOG.md`, `Makefile`, CI workflows, and packaging scripts.
2. Choose the next version:
   - prefer the existing versioning convention,
   - use `vMAJOR.MINOR.PATCH` for new SemVer projects,
   - mark prereleases clearly, such as `v1.2.0-rc.1`.
3. Run tests and normal release preflight.
4. Create or confirm an annotated Git tag at the exact commit to release.
5. Build release binaries from that tag or from a clean working tree at that commit.
6. Check reproducibility:
   - use the project's own reproducibility target if present,
   - otherwise build each binary twice with controlled flags and compare hashes.
7. Generate release files:
   - versioned archives or binaries,
   - `SHA256SUMS`,
   - `SHA256SUMS.asc` detached GPG signature,
   - optional per-artifact `.asc` signatures,
   - release notes.
8. Upload assets to a draft Codeberg release, then verify the displayed assets before publishing.

## References

Load only the reference needed for the job:

- `references/codeberg-release-api.md`: Codeberg/Forgejo tags, releases, access tokens, API publishing, assets.
- `references/go-binary-release.md`: Go build/version/reproducibility patterns and common cross-platform caveats.
- `references/gpg-signing.md`: GPG key selection, detached signatures, verification commands, private-key safety.
- `references/release-good-practices.md`: release notes, checksums, provenance, draft releases, tag hygiene.

Reusable helpers live in `scripts/`:

- `audit_release_readiness.sh`: read-only repo/tooling/GPG readiness inventory.
- `prepare_go_release.sh`: builds Go binaries twice, compares hashes, creates archives, writes `SHA256SUMS`, signs it with GPG, and writes release notes.
- `publish_codeberg_release.sh`: creates a draft Forgejo/Codeberg release through the API and uploads the prepared files.

Templates live in `assets/templates/` for Woodpecker and Forgejo Actions release checks. Adapt before copying.

## Safety Rules

- Never publish a final public release without explicit user approval.
- Prefer draft releases first.
- Never commit private keys, exported secret keys, tokens, passphrases, or signing material.
- Never put a GPG private key into CI unless the user explicitly accepts that risk.
- Do not echo API tokens or passphrases.
- Use the most restrictive Codeberg token that works; for release creation/assets, use repository-scoped `write:repository` when available.
- If the working tree has unrelated changes, do not clean, reset, or discard them. Ask before tagging if the release commit is ambiguous.
- Sign `SHA256SUMS`; sign each binary/archive too only when the project expects per-file signatures or the user requests them.

## Local Go Release Shortcut

For a Go project with a normal `main.version` linker variable:

```sh
sh .claude/skills/codeberg-release/scripts/audit_release_readiness.sh . vX.Y.Z
GPG_KEY=<key-id-or-fingerprint> \
  sh .claude/skills/codeberg-release/scripts/prepare_go_release.sh vX.Y.Z
```

Set these environment variables as needed:

- `BINARY_NAME`: output binary name.
- `MAIN_PACKAGE`: main package path, such as `./cmd/myapp`.
- `TARGETS`: space-separated `GOOS/GOARCH` pairs.
- `CGO_ENABLED`: set to `1` only for native/cgo-capable builds.
- `GO_LDFLAGS`: override default version/build-id flags.
- `SIGN_ARTIFACTS=1`: create detached `.asc` signatures for each archive.
- `EXPORT_GPG_PUBLIC_KEY=1`: include the public key for convenience, not as a trust anchor.

## Publishing Shortcut

After reviewing the generated release directory:

```sh
CODEBERG_TOKEN=<token> \
  sh .claude/skills/codeberg-release/scripts/publish_codeberg_release.sh OWNER REPO vX.Y.Z dist/release-vX.Y.Z
```

The publish helper creates a draft release by default. Set `DRAFT=0` only when the user has approved immediate publication.

## Verification Before Final Answer

- Confirm tests and reproducibility checks run, or explain why they could not.
- Confirm `SHA256SUMS` exists and has entries for release binaries/archives.
- Confirm `SHA256SUMS.asc` verifies locally.
- Confirm the release tag points at the intended commit.
- Confirm uploaded Codeberg release assets include binaries/archives, hashes, signatures, and notes.
- List any manual Codeberg settings still needed, such as enabling releases or creating a scoped token.

