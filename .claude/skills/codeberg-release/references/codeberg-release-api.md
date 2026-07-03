# Codeberg Release API

Use this reference when creating Codeberg releases or uploading release assets.

## Tags And Releases

On Codeberg and Forgejo:

- Git tags are repository snapshots.
- Releases are Forgejo objects linked to tags.
- Releases can contain notes, binary assets, and source archive links.
- Releases are created through the web interface or API, not through plain Git.
- Codeberg recommends version-number tags with a `v` prefix and SemVer-style numbering for projects that use SemVer.

Manual Codeberg setup:

- Repository releases may need to be enabled in `Settings` -> `Units` -> `Overview`.
- Create the Git tag before or during release creation.
- Draft releases are useful for reviewing generated notes, signatures, and assets before publication.

## API Basics

Codeberg exposes the Forgejo API at:

```text
https://codeberg.org/api/v1
```

Forgejo also exposes instance-specific Swagger/OpenAPI docs at:

```text
https://codeberg.org/api/swagger
https://codeberg.org/swagger.v1.json
```

Use the live API reference when exact fields matter.

Authentication header:

```text
Authorization: token <CODEBERG_TOKEN>
```

Access-token guidance:

- Create a token in Codeberg `Settings` -> `Applications`.
- Prefer repository-scoped access when available.
- For release and asset operations, use `write:repository`.
- Store the token in an environment variable or CI secret.
- Delete one-off release tokens after use.

## Release Endpoints

Forgejo documents these release operations:

```text
POST /repos/{owner}/{repo}/releases
POST /repos/{owner}/{repo}/releases/{id}/assets
```

Typical release payload:

```json
{
  "tag_name": "v1.2.3",
  "target_commitish": "main",
  "name": "v1.2.3",
  "body": "Release notes",
  "draft": true,
  "prerelease": false
}
```

Typical asset upload:

```sh
curl --fail --silent --show-error \
  -H "Authorization: token $CODEBERG_TOKEN" \
  -F "attachment=@dist/release-v1.2.3/app-v1.2.3-linux-amd64.tar.gz" \
  "https://codeberg.org/api/v1/repos/OWNER/REPO/releases/RELEASE_ID/assets?name=app-v1.2.3-linux-amd64.tar.gz"
```

## Upload Set

Upload:

- binary/archive assets,
- `SHA256SUMS`,
- `SHA256SUMS.asc`,
- per-file `.asc` signatures if generated,
- optional SBOM/provenance files if the project already maintains them.

Do not upload:

- private keys,
- token files,
- local config files,
- build caches,
- unreviewed debug logs.

## After Upload

Verify in the Codeberg UI:

- tag name,
- target commit,
- draft/published status,
- release notes,
- complete asset list,
- downloadable checksums/signatures.

Official docs to check when needed:

- Codeberg tags/releases: `https://docs.codeberg.org/git/using-tags/`
- Codeberg access tokens: `https://docs.codeberg.org/advanced/access-token/`
- Forgejo API usage: `https://forgejo.org/docs/latest/user/api-usage/`
- Forgejo token scopes: `https://forgejo.org/docs/latest/user/token-scope/`
- Forgejo releases: `https://forgejo.org/docs/latest/user/releases/`

