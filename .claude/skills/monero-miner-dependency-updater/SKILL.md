---
name: monero-miner-dependency-updater
description: Use this skill when monitoring, reviewing, updating, or creating pull requests for XMRig and P2Pool dependency releases in a Monero miner app, especially when reproducible builds, pinned hashes, security review, compatibility review, Codeberg PRs, and reproducible-builds.org practices matter.
---

# Monero Miner Dependency Updater

Use this skill to handle XMRig and P2Pool updates safely. The goal is not "latest at any cost"; the goal is a reviewed, pinned, reproducible, testable update PR.

## Default Flow

1. Inspect repo state:
   - `git status --short`
   - `rg -n "XMRIG_VERSION|P2POOL_VERSION|xmrig|p2pool|sha256|reproducible" scripts internal Makefile README.md REPRODUCIBLE.md .github .woodpecker .forgejo`
2. Detect upstream versions and release metadata:
   - `python3 .claude/skills/monero-miner-dependency-updater/scripts/check_upstream_releases.py --report-dir dependency-update-report --download-assets`
3. If no update exists, stop after reporting current and upstream versions.
4. Review release notes and upstream diffs before editing:
   - XMRig: miner donation behavior, config/CLI changes, RandomX changes, Stratum/pool behavior, thread control, network/TLS/proxy changes, CPU feature detection, asset naming, checksum/signature publication.
   - P2Pool: Stratum behavior, monerod RPC/ZMQ requirements, mini/main sidechain compatibility, Tor/I2P/peer networking, spam/DoS hardening, wallet/address handling, asset naming, checksum/signature publication.
5. If there are blatant security issues, unclear provenance, missing release assets, missing hashes, or no clean compatibility plan, do not update. Produce a report or issue instead.
6. If the update looks clean, apply pinned versions and hashes:
   - `python3 .claude/skills/monero-miner-dependency-updater/scripts/apply_dependency_update.py dependency-update-report/latest.json`
7. Run validation:
   - `sh .claude/skills/monero-miner-dependency-updater/scripts/run_update_validation.sh`
8. If the update remains clean, create a branch, commit the script/code changes and review report, push it, and create a Codeberg PR.

## References

Load only what is needed:

- `references/upstream-review.md`: security and compatibility review checklist for XMRig/P2Pool updates.
- `references/reproducible-builds.md`: how dependency updates must preserve reproducible-builds.org practices.
- `references/codeberg-pr.md`: Codeberg PR creation, tokens, branch naming, and PR body content.
- `references/project-surfaces.md`: repo-specific surfaces to check in this Monero miner app.

Reusable CI templates live in `assets/templates/`. Adapt before copying.

## Hard Gates

Do not create an update PR when any of these are true:

- Upstream release assets for a supported platform are missing and there is no explicit plan to drop or replace that platform.
- Checksums cannot be obtained from upstream metadata or calculated from downloaded assets.
- Upstream notes or diffs suggest wallet/address handling, donation behavior, miner networking, proxy/Tor behavior, or executable extraction changed in a way that needs code work you did not complete.
- The update makes release builds depend on an unpinned "latest" artifact.
- Reproducibility checks fail or cannot be run and the user asked for a clean update PR.
- The working tree contains unrelated changes that would be mixed into the dependency update commit.

## PR Standard

The PR should include:

- version bump summary for XMRig and/or P2Pool,
- upstream release links,
- security review summary,
- compatibility assessment,
- exact asset names and SHA256 hashes,
- tests and reproducibility checks run,
- any known limitations, such as native cgo build caveats,
- whether runtime autoinstall remains latest-based or was changed to pinned/hash-verified behavior.

Prefer one PR per dependency if both updates are non-trivial. A combined PR is acceptable when both are straightforward hash/version bumps and the validation surface is shared.

