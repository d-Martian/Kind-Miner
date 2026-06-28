# Release Good Practices

Use this reference for release hygiene beyond the mechanical build.

## Preflight Checklist

- Working tree is clean or all changes are intentionally included.
- Tests pass.
- Linters or vet tools pass if the project normally uses them.
- Version appears in code, docs, and packaging consistently.
- Release tag points to the intended commit.
- Release branch policy is respected.
- Dependencies are locked.
- Generated assets are reproducible or their limits are disclosed.

## Release Notes

Good release notes include:

- highlights,
- fixes,
- compatibility notes,
- security notes when relevant,
- upgrade steps,
- known issues,
- checksums/signature verification commands.

Do not dump noisy commit logs if the project normally writes curated notes. Use commit logs as a draft source.

## Draft First

Use draft releases when:

- humans must inspect binaries or notes,
- assets come from multiple machines,
- signing is manual,
- reproducibility matters,
- publication has security or privacy implications.

Publish immediately only when the user explicitly approves that behavior.

## Checksums And Signatures

Minimum release asset integrity set:

```text
app-vX.Y.Z-linux-amd64.tar.gz
app-vX.Y.Z-darwin-arm64.tar.gz
app-vX.Y.Z-windows-amd64.tar.gz
SHA256SUMS
SHA256SUMS.asc
```

Optional:

```text
app-vX.Y.Z-linux-amd64.tar.gz.asc
SBOM.spdx.json
provenance.intoto.jsonl
GPG-PUBLIC-KEY.asc
```

## Supply Chain Notes

- Pin toolchains for release builds.
- Avoid downloading dependencies during signing.
- Avoid mutable container tags for release-critical CI where practical.
- Keep release scripts in version control.
- Prefer dedicated release tokens.
- Rotate release tokens after emergency or one-off use.

## Verification Commands For Users

Include concise verification commands in the release notes when practical:

```sh
gpg --verify SHA256SUMS.asc SHA256SUMS
sha256sum -c SHA256SUMS
```

Mention the expected GPG fingerprint if the project has a public, trusted fingerprint.

