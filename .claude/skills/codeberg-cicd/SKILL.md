---
name: codeberg-cicd
description: Use this skill when working with Codeberg, Forgejo repositories, Codeberg Pages, Woodpecker CI, Forgejo Actions, releases, webhooks, access tokens, mirrors, or CI/CD migration from GitHub/GitLab to Codeberg.
---

# Codeberg CI/CD

Use this skill to make repository changes that fit Codeberg's Forgejo-based forge and CI/CD ecosystem.

## First pass

1. Inspect the repository before editing:
   - `git remote -v`
   - `find . -maxdepth 3 -type f \( -path './.woodpecker*' -o -path './.forgejo/workflows/*' -o -path './.github/workflows/*' -o -name 'Makefile' -o -name 'go.mod' -o -name 'package.json' -o -name 'pyproject.toml' \)`
   - `git status --short`
2. Identify the project's release path: tags, release notes, checksums, signatures, pages deployment, package registry, or external hosting.
3. Prefer existing build commands and project docs over inventing new ones.
4. Keep secrets out of committed files. Use Codeberg/Woodpecker secrets, Forgejo Actions secrets, or repository variables.

## CI choice

Prefer **Woodpecker CI** for hosted Codeberg CI. It is the normal Codeberg-hosted CI route and uses `.woodpecker.yml`, `.woodpecker.yaml`, or files in `.woodpecker/`.

Use **Forgejo Actions** when the repository already uses Actions, the user asks for Actions, Codeberg Pages requires the git-pages action, or the user has a self-hosted Forgejo runner. On Codeberg, hosted Actions availability can be limited; if hosted CI is required, suggest Woodpecker unless the user says otherwise.

Use **webhooks** when the project only needs to notify another service, trigger documentation builds, or integrate with a CI provider outside Codeberg.

## Reference Loading

Load only the reference needed for the task:

- `references/codeberg-platform.md`: repository settings, access tokens, releases, webhooks, Pages, migration notes.
- `references/woodpecker-ci.md`: Woodpecker syntax, secrets, labels, agents, release-oriented pipelines.
- `references/forgejo-actions.md`: Forgejo Actions syntax, runner constraints, Pages deployments, migration from GitHub Actions.
- `references/release-checklist.md`: tag/release flows, checksums, signing, reproducible build checks, safe publishing.

Reusable starting points live in `assets/templates/`. Copy them into the target repo only after adapting names, images, versions, commands, branches, paths, and secrets.

## Working Rules

- Preserve user changes. If CI files already exist, patch them instead of replacing them.
- Scope release automation to tags such as `refs/tags/v*` unless the project has a different convention.
- Gate deployments to the default branch or tags. Do not deploy from pull requests unless the user explicitly wants that risk and the workflow is hardened.
- For pull requests from forks, assume secrets are unavailable or unsafe. Do not use privileged tokens with untrusted code.
- Pin critical tool versions where reproducibility or release integrity matters.
- Add status badges only when the project already uses badges or the user asks.
- When moving from GitHub Actions, replace GitHub-only assumptions: `github.*` context, `GITHUB_TOKEN`, GitHub-hosted runners, GitHub release APIs, GitHub cache behavior, and marketplace actions.
- When using third-party actions or containers, prefer sources hosted on Codeberg/Forgejo or clearly maintained upstreams. Avoid random marketplace snippets.

## Common Outputs

For a basic project:

1. Add or update `.woodpecker.yml` with test, lint, and build steps.
2. Add tag-only release packaging if the repo already has release commands.
3. Add Pages deployment only for static sites or generated docs.
4. Document required secrets in comments inside the CI file only if the project style allows comments.
5. Validate syntax as far as local tooling allows, then summarize manual Codeberg setup steps.

For self-hosted runners:

1. Use labels to target the right OS/architecture/hardware.
2. Keep self-hosted runner setup instructions out of the pipeline file unless the user asks for docs.
3. Ensure long-running, privileged, or architecture-specific jobs do not land on shared hosted agents by accident.

## Verification

Before finishing:

- Run local tests/build commands if available.
- Run `sh .claude/skills/codeberg-cicd/scripts/audit_codeberg_ci.sh` from the repo root for a quick CI/CD inventory.
- Lint pipelines with `woodpecker-cli lint <file-or-.woodpecker/dir>` when the CLI is available.
- Check YAML indentation and branch/tag filters.
- Confirm any release or deployment step is gated and has named secrets.
- Tell the user which Codeberg settings they still need to enable manually, such as repository Actions, Woodpecker activation, Pages, releases, secrets, or self-hosted agents.

