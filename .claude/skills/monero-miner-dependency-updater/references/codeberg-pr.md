# Codeberg PR Process

Use this reference when turning a clean dependency update into a Codeberg pull request.

## Branches

Use one branch per update:

```text
deps/xmrig-v6.26.0
deps/p2pool-v4.17
deps/miner-runtime-deps-2026-06
```

Prefer separate PRs when either dependency has non-trivial compatibility changes. A combined PR is fine for a pure version/hash refresh.

## Commit Contents

Include only related files:

- dependency download scripts or manifests,
- compatibility code changes,
- tests,
- update report if the project wants it committed,
- CI workflow changes for dependency monitoring.

Do not include unrelated packaging edits, local reports with secrets, downloaded archives, build outputs, or `dist/`.

## PR Body

Include:

- dependency versions before and after,
- upstream release URLs,
- security review notes,
- compatibility review notes,
- asset filenames and SHA256 hashes,
- test commands and results,
- reproducibility commands and results,
- manual follow-up items.

## API Creation

The helper script uses Codeberg's Forgejo API:

```sh
CODEBERG_TOKEN=... \
python3 .claude/skills/monero-miner-dependency-updater/scripts/create_codeberg_pr.py \
  dMartian kind-miner deps/xmrig-v6.26.0 main \
  "Update XMRig to v6.26.0" dependency-update-report/PR_BODY.md
```

Use a token scoped to the target repository. Do not print the token or commit it.

