# Release Checklist

Use this reference when adding or reviewing release automation for Codeberg.

## Preflight

- Confirm the project has an explicit version source: tag, `VERSION`, package metadata, or `git describe`.
- Confirm release assets are built from the tag commit.
- Confirm local build flags match CI build flags when reproducibility matters.
- Confirm release artifacts include checksums.
- Confirm signatures are detached from reproducible payloads when signing is enabled.
- Confirm the release workflow does not run on arbitrary branches or pull requests.

## Tagging

Recommended manual tag flow:

```sh
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

Use semantic versions with a `v` prefix unless the project already uses another convention.

## CI Release Gates

Woodpecker:

```yaml
when:
  - event: tag
    ref: refs/tags/v*
```

Forgejo Actions:

```yaml
on:
  push:
    tags:
      - 'v*'
```

## Artifacts

Good release assets:

- platform-specific archives,
- checksums (`SHA256SUMS` or per-file `.sha256`),
- detached signatures (`.asc` or `.sig`) if configured,
- source-generated docs or SBOMs when the project already ships them.

Avoid:

- host-specific paths in binaries,
- mutable `latest` release-only artifacts without versioned copies,
- publishing secrets or private config files,
- release jobs that overwrite assets silently.

## Draft Versus Published

Use draft releases when:

- humans must review generated notes,
- signing happens outside CI,
- assets come from multiple platforms or runners,
- the project publishes privacy/security-sensitive binaries.

Publish automatically only when:

- tests are comprehensive,
- artifacts are deterministic enough for the project,
- rollback or deletion behavior is understood,
- maintainers want unattended releases.

## Reproducible Build Projects

For projects with reproducibility claims:

- Keep build flags identical across `Makefile`, scripts, docs, and CI.
- Set `SOURCE_DATE_EPOCH` from the tag commit time when packaging uses timestamps.
- Pin toolchains.
- Keep signing outside byte-for-byte reproducibility claims.
- Include a local reproducibility check in CI when practical, but do not let it replace cross-host verification if the project claims that.

