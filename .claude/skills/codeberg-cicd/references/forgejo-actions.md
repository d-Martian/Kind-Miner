# Forgejo Actions

Use this reference for `.forgejo/workflows/*`, self-hosted runners, GitHub Actions migration, and Codeberg Pages deployment.

## File Location

Forgejo Actions workflows belong in:

```text
.forgejo/workflows/*.yml
.forgejo/workflows/*.yaml
```

GitHub-style `.github/workflows/*` may exist in migrated repos. Do not assume Codeberg will run them as-is.

## Codeberg Runner Reality

On Codeberg, hosted Forgejo Actions can be limited. Use Actions when:

- the user has a self-hosted Forgejo runner,
- the repo already uses Actions,
- Codeberg Pages deployment with `git-pages/action` is requested,
- a GitHub Actions workflow needs a close Forgejo-native port.

If the user asks for hosted Codeberg CI and does not require Actions compatibility, use Woodpecker.

## Basic Workflow

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: docker
    container:
      image: golang:1.25
    steps:
      - uses: https://code.forgejo.org/actions/checkout@v4
      - run: go test ./...
```

Adapt `runs-on` to the labels exposed by the target runner. Self-hosted runners may use different labels such as `docker`, `linux-amd64`, or project-specific labels.

## Forgejo Contexts

Prefer Forgejo-native context names when available:

- `forge.repository`
- `forge.repository_owner`
- `forge.ref`
- `forge.token`

Migration notes:

- Replace `github.ref` with `forge.ref` when the workflow is Forgejo-specific.
- Replace `GITHUB_TOKEN` assumptions with Forgejo's automatic token or a named secret.
- Check whether actions referenced from GitHub Marketplace are usable on Forgejo. Prefer actions hosted on `code.forgejo.org` or `codeberg.org` when available.

## Security

- Avoid `pull_request_target` unless the workflow does not check out or run untrusted pull request code.
- In pull requests from forks, assume secrets are unavailable and tokens are read-only.
- Do not run deploy, release, package publish, or Pages steps on pull requests.
- If using checkout in privileged contexts, configure credentials carefully and avoid persisting tokens into untrusted workspaces.

## Pages Deployment

Use this pattern after a static site build step:

```yaml
- uses: https://codeberg.org/git-pages/action@v2
  if: ${{ forge.ref == 'refs/heads/main' }}
  with:
    site: 'https://${{ forge.repository_owner }}.codeberg.page/repository-name/'
    token: ${{ forge.token }}
    source: public/
```

Adapt:

- branch (`main` or project default),
- repository name in the URL,
- output directory (`public/`, `_site/`, `dist/`, etc.).

## Tag Releases

Forgejo Actions can trigger on tags:

```yaml
on:
  push:
    tags:
      - 'v*'
```

For release uploads, prefer maintained Forgejo-compatible actions or a project-owned script. Verify the target instance API before writing raw `curl` release calls.

## Templates

Use:

- `assets/templates/forgejo-actions-go-ci.yml` for a Go test/build workflow.
- `assets/templates/forgejo-actions-pages.yml` for Codeberg Pages deployment.

Official docs:

- Codeberg Actions guide: `https://docs.codeberg.org/ci/actions/`
- Forgejo Actions reference: `https://forgejo.org/docs/latest/user/actions/reference/`
- Codeberg Pages with Forgejo Actions: `https://docs.codeberg.org/codeberg-pages/forgejo-actions/`

