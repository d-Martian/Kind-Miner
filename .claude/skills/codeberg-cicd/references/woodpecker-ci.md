# Woodpecker CI

Use this reference for hosted Codeberg CI and `.woodpecker*` pipeline work.

## File Locations

Common layouts:

- `.woodpecker.yml`
- `.woodpecker.yaml`
- `.woodpecker/*.yml`
- `.woodpecker/*.yaml`

Use one file for simple projects. Use a `.woodpecker/` directory when separating CI, release, pages, or platform-specific workflows improves readability.

## Basic Syntax

Woodpecker steps run in containers. Steps run serially by default, and a non-zero command exits the workflow.

Minimal pattern:

```yaml
steps:
  - name: test
    image: golang:1.25
    commands:
      - go test ./...
```

Useful keys:

- `steps`: build/test/deploy steps.
- `image`: container image for a step.
- `commands`: shell commands for build steps.
- `when`: branch, event, ref, path, status, matrix, or expression filters.
- `depends_on`: make a DAG and run independent steps in parallel.
- `matrix`: run combinations of variables.
- `labels`: select self-hosted agents.
- `services`: database or other service containers.
- `environment`: plain environment values or secrets.

## Events And Gates

Common gates:

```yaml
when:
  - event: push
    branch: main
```

```yaml
when:
  - event: tag
    ref: refs/tags/v*
```

```yaml
when:
  - event: pull_request
```

Guidelines:

- Use `event: tag` plus `ref: refs/tags/v*` for releases.
- Use `event: push` plus the default branch for deployments.
- Avoid deployment secrets in pull request events.
- Remember branch filters do not apply to tag events.

## Pin Images By Digest

Codeberg's official examples repo pins every step/plugin image by digest, e.g.
`image: cytopia/yamllint:alpine-1@sha256:4fb4...`. Prefer this for release,
publishing, and reproducibility-sensitive steps: a floating tag silently swaps
the build environment under you. Pin `image: name:tag@sha256:<digest>` and bump
deliberately (Renovate, which that repo uses, can automate the updates). This is
the same input-pinning discipline the `reproducible-builds-review` skill applies
to dependencies and base images.

## Built-in Variables

Prefer Woodpecker's built-in metadata over hardcoding owner/branch/tag:

- `${CI_REPO_OWNER}` / `${CI_REPO_NAME}` — owner and repository.
- `${CI_REPO_DEFAULT_BRANCH}` — gate deploys without hardcoding `main`.
- `${CI_COMMIT_TAG}` — the tag on tag events (release version and asset names).
- `${CI_COMMIT_BRANCH}`, `${CI_COMMIT_SHA}`, `${CI_PIPELINE_EVENT}`.

YAML anchors keep multi-step pipelines DRY and keep a pinned image in one place:

```yaml
variables:
  - &golang golang:1.25@sha256:<digest>
steps:
  test:  { image: *golang, commands: [go test ./...] }
  build: { image: *golang, commands: [go build ./...] }
```

## Secrets

Declare secrets in Woodpecker's UI or CLI. Reference them as environment values.

Pattern:

```yaml
steps:
  - name: publish
    image: alpine:3.20
    environment:
      CODEBERG_TOKEN:
        from_secret: codeberg_token
    commands:
      - test -n "$CODEBERG_TOKEN"
```

Rules:

- Do not echo secrets.
- Keep token-bearing commands quiet where possible.
- Prefer built-in CI metadata before adding custom variables.

## Self-Hosted Agents

Use self-hosted agents for specialized OS/architecture builds, privileged packaging, hardware tests, faster feedback, or long jobs.

Agent selection:

```yaml
labels:
  platform: linux/amd64
```

Custom labels:

```yaml
labels:
  capability: appimage
  location: lab
```

Codeberg's Woodpecker agent docs describe adding agents with `WOODPECKER_SERVER=grpc.ci.codeberg.org:443`, `WOODPECKER_GRPC_SECURE=true`, and `WOODPECKER_AGENT_SECRET` from Codeberg CI. Keep that token secret.

## Go Project Template

Use `assets/templates/woodpecker-go.yml` as a starting point for Go projects. Adapt:

- Go version.
- Linux build dependency packages.
- `make` targets.
- release archive names.
- secret names.
- branch and tag filters.

## Release Uploads

For release publishing, prefer a maintained Forgejo-compatible CLI or a small project-owned script that calls the Forgejo API. Check the live API docs on the target Codeberg instance before writing exact endpoints.

Release automation should:

- build from a tag,
- produce checksums,
- fail if artifacts are missing,
- avoid overwriting published release assets unless explicitly intended,
- use a dedicated token secret if the CI-provided token cannot publish the release.

## Container Registry Publish (buildx)

Publish multi-arch images to Codeberg's container registry with the docker-buildx
plugin. Dry-run on pull requests, publish on push to the default branch:

```yaml
steps:
  dryrun:
    image: docker.io/woodpeckerci/plugin-docker-buildx:latest
    settings:
      repo: codeberg.org/${CI_REPO_OWNER}/<image>
      platforms: linux/amd64,linux/arm64
      dry_run: true
      tags: latest
    when: { event: pull_request }
  publish:
    image: docker.io/woodpeckerci/plugin-docker-buildx:latest
    settings:
      repo: codeberg.org/${CI_REPO_OWNER}/<image>
      registry: codeberg.org
      platforms: linux/amd64,linux/arm64
      tags: latest
      username: ${CI_REPO_OWNER}
      password: { from_secret: cb_token }
    when: { event: push, branch: "${CI_REPO_DEFAULT_BRANCH}" }
```

Create a Codeberg package-registry token and store it as the `cb_token` secret.
The plugin also takes a `logins:` list to push to several registries at once.

## Local Validation

Lint pipelines with the Woodpecker CLI before pushing — this is exactly what the
Codeberg examples repo runs in its own CI:

```sh
woodpecker-cli lint .woodpecker.yaml   # a single file
woodpecker-cli lint .woodpecker/       # every file in the directory
```

If the CLI is unavailable, still check:

- YAML parses,
- required commands exist locally or in the chosen container,
- tag and branch filters match the intended publishing flow,
- secrets are named but not committed.

Official docs:

- Workflow syntax: `https://woodpecker-ci.org/docs/usage/workflow-syntax`
- Secrets: `https://woodpecker-ci.org/docs/usage/secrets`
- Codeberg agents: `https://docs.codeberg.org/ci/agents/`
- Official examples (per-language pipelines, digest-pinned, lint-in-CI): `https://codeberg.org/Codeberg-CI/examples`

