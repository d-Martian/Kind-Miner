# GPG Signing

Use this reference when selecting keys, signing checksums, and verifying release signatures.

## Private Key Safety

Default to local signing. CI signing requires storing a private key and passphrase in CI secrets, which is a material security tradeoff.

Never commit:

- private keys,
- secret key exports,
- passphrases,
- token files,
- generated GnuPG home directories.

## Key Selection

List secret keys:

```sh
gpg --list-secret-keys --keyid-format LONG
```

Use a long key ID or full fingerprint:

```sh
GPG_KEY=0123456789ABCDEF
```

Codeberg supports adding and verifying a public GPG key in account settings. Codeberg's GPG guide recommends using the same email as the Codeberg account for Git signing.

## Signing Checksums

Preferred release signature:

```sh
sha256sum artifact-* > SHA256SUMS
gpg --armor --detach-sign --local-user "$GPG_KEY" -o SHA256SUMS.asc SHA256SUMS
```

Verification:

```sh
gpg --verify SHA256SUMS.asc SHA256SUMS
sha256sum -c SHA256SUMS
```

On macOS, use:

```sh
shasum -a 256 -c SHA256SUMS
```

## Per-Artifact Signatures

Optional:

```sh
gpg --armor --detach-sign --local-user "$GPG_KEY" artifact.tar.gz
```

This creates `artifact.tar.gz.asc`. It is useful when users download one binary and one signature without `SHA256SUMS`, but it creates more files to manage.

## Public Key Asset

Including `GPG-PUBLIC-KEY.asc` in release assets can help users find the key, but it is not a trust anchor by itself. Tell users to verify the fingerprint against a trusted source such as Codeberg account GPG settings, a website, a signed Git tag, or previous releases.

## Signed Tags

If the project wants signed Git tags:

```sh
git tag -s vX.Y.Z -m "vX.Y.Z"
```

If it only wants annotated tags:

```sh
git tag -a vX.Y.Z -m "vX.Y.Z"
```

Do not silently change an existing project's tag policy.

Official doc:

- Codeberg GPG keys: `https://docs.codeberg.org/security/gpg-key/`

