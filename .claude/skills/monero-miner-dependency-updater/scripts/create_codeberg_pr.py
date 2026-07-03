#!/usr/bin/env python3
"""Create a Codeberg/Forgejo pull request for a prepared dependency branch."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request
from pathlib import Path


def ensure_clean(allow_dirty: bool) -> None:
    if allow_dirty:
        return
    result = subprocess.run(["git", "status", "--porcelain"], text=True, capture_output=True, check=True)
    if result.stdout.strip():
        raise RuntimeError("Working tree is dirty. Commit only the dependency update files first.")


def push_branch(branch: str) -> None:
    subprocess.run(["git", "push", "-u", "origin", branch], check=True)


def api_post(url: str, token: str, payload: dict) -> dict:
    request = urllib.request.Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Authorization": f"token {token}",
            "Content-Type": "application/json",
            "Accept": "application/json",
            "User-Agent": "kind-miner-dependency-updater",
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"Codeberg API error {exc.code}: {detail}") from exc


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("owner")
    parser.add_argument("repo")
    parser.add_argument("head", help="Dependency update branch name")
    parser.add_argument("base", help="Target branch, usually main")
    parser.add_argument("title")
    parser.add_argument("body_file")
    parser.add_argument("--base-url", default="https://codeberg.org")
    parser.add_argument("--push", action="store_true", help="Push head branch before creating the PR")
    parser.add_argument("--allow-dirty", action="store_true")
    args = parser.parse_args(argv)

    token = os.environ.get("CODEBERG_TOKEN")
    if not token:
        raise RuntimeError("CODEBERG_TOKEN is required")

    body_path = Path(args.body_file)
    if not body_path.is_file():
        raise RuntimeError(f"PR body file not found: {body_path}")

    ensure_clean(args.allow_dirty)
    if args.push:
        push_branch(args.head)

    payload = {
        "base": args.base,
        "head": args.head,
        "title": args.title,
        "body": body_path.read_text(encoding="utf-8"),
    }
    url = f"{args.base_url.rstrip('/')}/api/v1/repos/{args.owner}/{args.repo}/pulls"
    response = api_post(url, token, payload)
    print(response.get("html_url") or response.get("url") or json.dumps(response, indent=2))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(1)

