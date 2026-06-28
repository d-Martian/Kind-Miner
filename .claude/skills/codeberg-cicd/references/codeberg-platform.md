# Codeberg Platform Notes

Use this reference for repository setup, releases, access tokens, Pages, webhooks, and migration work.

## Current Platform Shape

- Codeberg is a Forgejo-based public forge. Repository features include issues, pull requests, releases, packages, webhooks, wiki, Codeberg Pages, Weblate integration, Woodpecker CI, and Forgejo Actions.
- Codeberg's own documentation points to Forgejo docs for general forge behavior and Woodpecker docs for CI syntax when Codeberg-specific pages are thin.
- Hosted Forgejo Actions on Codeberg can be limited. Treat Woodpecker as the hosted CI default unless the user has a self-hosted runner or specifically requests Actions.

## Repository Settings

Common manual setup points:

- Enable releases in repository settings before publishing releases through the UI.
- Enable Actions in repository settings before Forgejo Actions workflows can run.
- Activate Woodpecker for the repository in `https://ci.codeberg.org` before Woodpecker pipelines run.
- Add secrets in the CI system, not in the repository.
- Add deploy keys or access tokens only with the narrowest practical scope and rotate them when no longer needed.

## Access Tokens

Use access tokens for API automation only when the platform-provided CI token is insufficient.

Guidelines:

- Prefer per-purpose tokens.
- Store tokens as secrets.
- Never print tokens or include them in command-line arguments that logs might expose.
- Delete tokens after one-off migrations or release repair tasks.

Typical secret names:

- `CODEBERG_TOKEN`: API token for release/package work.
- `PAGES_TOKEN`: only if the workflow cannot use the built-in Forgejo token.
- `REGISTRY_TOKEN`: package or container registry access.

## Releases

Tags are Git objects. Releases are Forgejo objects linked to tags and may have notes and binary assets.

Recommended release flow:

1. Create annotated tags for human releases: `git tag -a vX.Y.Z -m "vX.Y.Z"`.
2. Push the specific tag: `git push origin vX.Y.Z`.
3. Let CI build release artifacts on tag events.
4. Generate checksums and, when configured, detached signatures.
5. Publish releases as drafts when the workflow supports it and the project wants review before final publishing.

Avoid building release assets from mutable branches unless the project intentionally has nightly artifacts.

## Codeberg Pages

Use Codeberg Pages for static sites and generated docs. Pages deployment can be handled by Forgejo Actions with `https://codeberg.org/git-pages/action@v2`.

Rules:

- Deploy from the default branch or tags, not arbitrary pull requests.
- Keep the generated site directory explicit, such as `_site/`, `public/`, `dist/`, or `site/`.
- For user or organization pages, a repository named `pages` can publish at `https://USER.codeberg.page/`.
- For project pages, include the repository name in the site URL: `https://USER.codeberg.page/REPOSITORY/`.

## Webhooks

Use webhooks for small integrations: documentation rebuilds, notifications, external CI triggers, package index refreshes, or deployment services.

Checklist:

- Choose the Forgejo webhook template when the service only needs a generic POST.
- Limit events to what the integration needs.
- Use branch filters for production-only hooks.
- Use a shared secret when the receiving service supports it.
- Test delivery from the webhook settings after saving.

## Migration From GitHub Or GitLab

When migrating:

- Replace remote URLs, badges, clone URLs, issue links, and release links.
- Convert `.github/workflows/*` to Woodpecker or Forgejo Actions instead of assuming perfect compatibility.
- Replace GitHub release upload steps with Forgejo-compatible tooling or API calls.
- Replace GitHub Pages assumptions with Codeberg Pages.
- Audit secrets and tokens; do not copy old CI secrets blindly.

Useful official docs:

- Codeberg docs: `https://docs.codeberg.org/`
- Forgejo user docs: `https://forgejo.org/docs/latest/user/`
- Woodpecker docs: `https://woodpecker-ci.org/docs/`

