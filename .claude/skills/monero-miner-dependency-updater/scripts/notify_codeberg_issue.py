#!/usr/bin/env python3
"""Open — idempotently — a Codeberg issue when an XMRig/P2Pool update exists.

Reads the report written by check_upstream_releases.py and, if any dependency
has an update, ensures a single open tracking issue exists. The issue title
encodes the target versions, so repeated cron runs do not create duplicates; a
newer upstream version yields a new title and therefore a fresh issue.

This is the notify half of Stage 1 (cheap, no AI): detection + a durable nudge.
The actual review/update/PR (Stage 2) is done by a human running the
monero-miner-dependency-updater skill — never auto-merged.

Usage: CODEBERG_TOKEN=... notify_codeberg_issue.py <owner> <repo> <latest.json>
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path

API = "https://codeberg.org/api/v1"


def req(url: str, token: str, method: str = "GET", payload: dict | None = None) -> object:
    data = json.dumps(payload).encode("utf-8") if payload is not None else None
    headers = {
        "Authorization": f"token {token}",
        "Accept": "application/json",
        "User-Agent": "kind-miner-dependency-updater",
    }
    if data is not None:
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=60) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"Codeberg API {exc.code}: {detail}") from exc


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("owner")
    parser.add_argument("repo")
    parser.add_argument("report_json")
    parser.add_argument("--base-url", default=API)
    args = parser.parse_args(argv)

    token = os.environ.get("CODEBERG_TOKEN")
    if not token:
        raise RuntimeError("CODEBERG_TOKEN is required")

    report = json.loads(Path(args.report_json).read_text(encoding="utf-8"))
    updates = {n: d for n, d in report["dependencies"].items() if d.get("update_available")}
    if not updates:
        print("No dependency updates; no issue needed.")
        return 0

    parts = [f"{n} {d['current_version']}→{d['latest_version']}" for n, d in updates.items()]
    title = "[deps] update available: " + ", ".join(parts)
    base = args.base_url.rstrip("/")

    existing = req(f"{base}/repos/{args.owner}/{args.repo}/issues?state=open&type=issues&limit=50", token)
    for issue in existing:
        if isinstance(issue, dict) and issue.get("title") == title:
            print(f"Issue already open: {issue.get('html_url')}")
            return 0

    lines = ["Automated dependency watch found new upstream release(s).", ""]
    for name, dep in updates.items():
        lines.append(f"### {name}: `{dep['current_version']}` → `{dep['latest_version']}`")
        release = dep.get("release") or {}
        if release.get("html_url"):
            lines.append(f"- Release: {release['html_url']}")
        if dep.get("compare"):
            lines.append(f"- Compare: {dep['compare'].get('html_url')}")
        if dep.get("missing_platforms"):
            lines.append(f"- ⚠ Missing platform assets: {', '.join(dep['missing_platforms'])}")
        lines.append("")
    lines += [
        "---",
        "Review + update with the `monero-miner-dependency-updater` skill:",
        "security & compatibility review against the hard gates, then",
        "`apply_dependency_update.py`, `run_update_validation.sh`, and open a PR.",
        "Do not auto-merge — a human approves miner dependency changes.",
    ]

    created = req(
        f"{base}/repos/{args.owner}/{args.repo}/issues",
        token,
        method="POST",
        payload={"title": title, "body": "\n".join(lines)},
    )
    print(f"Opened issue: {created.get('html_url') if isinstance(created, dict) else created}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(1)
