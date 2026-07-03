#!/usr/bin/env python3
"""Check upstream XMRig/P2Pool releases and write an update review report."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
import re
import subprocess
import sys
import textwrap
import urllib.error
import urllib.request
from pathlib import Path


DEPS = {
    "xmrig": {
        "repo": "xmrig/xmrig",
        "script": "scripts/download-xmrig.sh",
        "version_var": "XMRIG_VERSION",
        "platforms": {
            # XMRig publishes no Linux ARM64 prebuilt (only linux-static-x64 and
            # distro-specific x64 builds), so linux-arm64 is intentionally not
            # tracked — listing it would permanently trip the missing-asset gate.
            "linux-amd64": [
                "xmrig-{version}-linux-static-x64.tar.gz",
            ],
            "darwin-amd64": [
                "xmrig-{version}-macos-x64.tar.gz",
            ],
            "darwin-arm64": [
                "xmrig-{version}-macos-arm64.tar.gz",
            ],
            "windows-amd64": [
                "xmrig-{version}-msvc-win64.zip",
                "xmrig-{version}-windows-x64.zip",
                "xmrig-{version}-windows-gcc-x64.zip",
            ],
        },
    },
    "p2pool": {
        "repo": "SChernykh/p2pool",
        "script": "scripts/download-p2pool.sh",
        "version_var": "P2POOL_VERSION",
        "platforms": {
            "linux-amd64": [
                "p2pool-v{version}-linux-x64.tar.gz",
            ],
            "linux-arm64": [
                "p2pool-v{version}-linux-aarch64.tar.gz",
            ],
            "darwin-amd64": [
                "p2pool-v{version}-macos-x64.tar.gz",
            ],
            "darwin-arm64": [
                "p2pool-v{version}-macos-aarch64.tar.gz",
            ],
            "windows-amd64": [
                "p2pool-v{version}-windows-x64.zip",
            ],
        },
    },
}


def repo_root() -> Path:
    try:
        out = subprocess.check_output(
            ["git", "rev-parse", "--show-toplevel"], text=True, stderr=subprocess.DEVNULL
        )
        return Path(out.strip())
    except Exception:
        return Path.cwd()


def normalize_version(tag_or_version: str) -> str:
    return tag_or_version.strip().lstrip("v")


def current_version(root: Path, dep: dict) -> str:
    script = root / dep["script"]
    text = script.read_text(encoding="utf-8")
    pattern = r'^{}="([^"]+)"'.format(re.escape(dep["version_var"]))
    match = re.search(pattern, text, flags=re.MULTILINE)
    if not match:
        raise RuntimeError(f"Could not find {dep['version_var']} in {script}")
    return match.group(1)


def current_sums(root: Path, dep: dict) -> dict[str, str]:
    script = root / dep["script"]
    text = script.read_text(encoding="utf-8")
    sums: dict[str, str] = {}
    in_sums = False
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith("declare -A SUMS=("):
            in_sums = True
            continue
        if in_sums and stripped == ")":
            break
        if in_sums:
            match = re.match(r'\["([^"]+)"\]="([^"]*)"', stripped)
            if match:
                sums[match.group(1)] = match.group(2)
    return sums


def request_headers() -> dict[str, str]:
    headers = {
        "Accept": "application/vnd.github+json",
        "User-Agent": "kind-miner-dependency-updater",
        "X-GitHub-Api-Version": "2022-11-28",
    }
    token = os.environ.get("GITHUB_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    return headers


def fetch_json(url: str) -> dict:
    request = urllib.request.Request(url, headers=request_headers())
    try:
        with urllib.request.urlopen(request, timeout=45) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"HTTP {exc.code} for {url}: {detail}") from exc


def fetch_latest(repo: str) -> dict:
    return fetch_json(f"https://api.github.com/repos/{repo}/releases/latest")


def fetch_compare(repo: str, current: str, latest: str) -> dict | None:
    if current == latest:
        return None
    url = f"https://api.github.com/repos/{repo}/compare/v{current}...v{latest}"
    try:
        return fetch_json(url)
    except RuntimeError:
        return None


def checksum_map_from_body(body: str) -> dict[str, str]:
    checksums: dict[str, str] = {}
    for line in body.splitlines():
        match = re.search(r"\b([a-fA-F0-9]{64})\s+\*?([A-Za-z0-9._+@:/=-]+)", line)
        if match:
            checksums[match.group(2)] = match.group(1).lower()
    return checksums


def select_asset(assets: list[dict], candidates: list[str], version: str) -> tuple[dict | None, list[str]]:
    by_name = {asset.get("name", ""): asset for asset in assets}
    rendered = [candidate.format(version=version) for candidate in candidates]
    for name in rendered:
        if name in by_name:
            return by_name[name], rendered
    return None, rendered


def github_asset_sha(asset: dict) -> str | None:
    digest = asset.get("digest") or ""
    if digest.startswith("sha256:"):
        return digest.split(":", 1)[1].lower()
    return None


def download_sha256(url: str, dest: Path) -> str:
    request = urllib.request.Request(url, headers=request_headers())
    digest = hashlib.sha256()
    with urllib.request.urlopen(request, timeout=300) as response:
        dest.parent.mkdir(parents=True, exist_ok=True)
        with dest.open("wb") as out:
            while True:
                chunk = response.read(1024 * 1024)
                if not chunk:
                    break
                digest.update(chunk)
                out.write(chunk)
    return digest.hexdigest()


def analyze_dep(root: Path, name: str, dep: dict, report_dir: Path, download_assets: bool) -> dict:
    current = current_version(root, dep)
    local_sums = current_sums(root, dep)
    release = fetch_latest(dep["repo"])
    latest = normalize_version(release.get("tag_name", ""))
    body = release.get("body") or ""
    body_sums = checksum_map_from_body(body)
    assets = release.get("assets") or []
    compare = fetch_compare(dep["repo"], current, latest)

    selected: dict[str, dict] = {}
    missing: list[str] = []
    warnings: list[str] = []

    for platform, candidates in dep["platforms"].items():
        asset, rendered = select_asset(assets, candidates, latest)
        if not asset:
            missing.append(platform)
            selected[platform] = {
                "platform": platform,
                "present": False,
                "candidates": rendered,
            }
            continue

        asset_name = asset["name"]
        sha_source = "missing"
        sha = github_asset_sha(asset)
        if sha:
            sha_source = "github_asset_digest"
        elif asset_name in body_sums:
            sha = body_sums[asset_name]
            sha_source = "release_body"

        downloaded_path = None
        if download_assets:
            dest = report_dir / "downloads" / name / asset_name
            calculated = download_sha256(asset["browser_download_url"], dest)
            downloaded_path = str(dest)
            if sha and sha != calculated:
                warnings.append(
                    f"{name} {platform}: upstream hash {sha} does not match downloaded hash {calculated}"
                )
            sha = calculated
            sha_source = "downloaded_asset"

        if not sha:
            warnings.append(f"{name} {platform}: no SHA256 available for {asset_name}")

        selected[platform] = {
            "platform": platform,
            "present": True,
            "name": asset_name,
            "url": asset.get("browser_download_url"),
            "size": asset.get("size"),
            "sha256": sha,
            "sha256_source": sha_source,
            "downloaded_path": downloaded_path,
            "candidates": rendered,
            "current_pinned_sha256": local_sums.get(platform),
        }

    compare_summary = None
    if compare:
        compare_summary = {
            "html_url": compare.get("html_url"),
            "status": compare.get("status"),
            "ahead_by": compare.get("ahead_by"),
            "behind_by": compare.get("behind_by"),
            "total_commits": compare.get("total_commits"),
            "files": [
                {
                    "filename": item.get("filename"),
                    "status": item.get("status"),
                    "changes": item.get("changes"),
                }
                for item in (compare.get("files") or [])[:200]
            ],
        }

    return {
        "name": name,
        "repo": dep["repo"],
        "script": dep["script"],
        "version_var": dep["version_var"],
        "current_version": current,
        "latest_version": latest,
        "update_available": current != latest,
        "release": {
            "tag_name": release.get("tag_name"),
            "name": release.get("name"),
            "html_url": release.get("html_url"),
            "published_at": release.get("published_at"),
            "target_commitish": release.get("target_commitish"),
            "body": body,
        },
        "selected_assets": selected,
        "missing_platforms": missing,
        "warnings": warnings,
        "compare": compare_summary,
    }


def md_escape(text: str) -> str:
    return text.replace("|", "\\|")


def short_body(body: str, limit: int = 2400) -> str:
    body = body.strip()
    if len(body) <= limit:
        return body
    return body[:limit].rstrip() + "\n\n[truncated]"


def write_markdown(report: dict, path: Path) -> None:
    lines: list[str] = []
    lines.append("# Miner Dependency Update Review")
    lines.append("")
    lines.append(f"Generated: `{report['generated_at']}`")
    lines.append("")

    for dep in report["dependencies"].values():
        lines.append(f"## {dep['name']}")
        lines.append("")
        lines.append(f"- Repo: `{dep['repo']}`")
        lines.append(f"- Current: `{dep['current_version']}`")
        lines.append(f"- Latest: `{dep['latest_version']}`")
        lines.append(f"- Update available: `{str(dep['update_available']).lower()}`")
        lines.append(f"- Release: {dep['release'].get('html_url') or ''}")
        if dep.get("compare"):
            lines.append(f"- Compare: {dep['compare'].get('html_url') or ''}")
        lines.append("")

        lines.append("### Selected Assets")
        lines.append("")
        lines.append("| Platform | Asset | SHA256 | Source |")
        lines.append("|---|---|---|---|")
        for platform, asset in dep["selected_assets"].items():
            if not asset.get("present"):
                candidates = ", ".join(asset.get("candidates", []))
                lines.append(f"| `{platform}` | missing; tried `{md_escape(candidates)}` |  |  |")
                continue
            lines.append(
                "| `{}` | `{}` | `{}` | `{}` |".format(
                    platform,
                    md_escape(asset.get("name", "")),
                    asset.get("sha256") or "",
                    asset.get("sha256_source") or "",
                )
            )
        lines.append("")

        if dep["missing_platforms"]:
            lines.append("### Blockers")
            lines.append("")
            for platform in dep["missing_platforms"]:
                lines.append(f"- Missing supported-platform asset: `{platform}`")
            lines.append("")

        if dep["warnings"]:
            lines.append("### Warnings")
            lines.append("")
            for warning in dep["warnings"]:
                lines.append(f"- {warning}")
            lines.append("")

        lines.append("### Release Notes Excerpt")
        lines.append("")
        excerpt = short_body(dep["release"].get("body") or "")
        if excerpt:
            lines.append("```text")
            lines.append(excerpt)
            lines.append("```")
        else:
            lines.append("_No release notes body returned by API._")
        lines.append("")

        lines.append("### Review Prompts")
        lines.append("")
        lines.append("- Any wallet/address, donation, proxy/Tor, Stratum, RPC, or network default changes?")
        lines.append("- Any supported platform asset missing or renamed?")
        lines.append("- Do pinned hashes match downloaded artifacts?")
        lines.append("- Does runtime autoinstall need code changes to stay pinned/hash-verified?")
        lines.append("- Were `go test ./...` and reproducibility checks run after applying the update?")
        lines.append("")

    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def write_env(report: dict, path: Path) -> None:
    lines = []
    for name, dep in report["dependencies"].items():
        prefix = name.upper().replace("-", "_")
        lines.append(f"{prefix}_CURRENT={dep['current_version']}")
        lines.append(f"{prefix}_LATEST={dep['latest_version']}")
        lines.append(f"{prefix}_UPDATE_AVAILABLE={str(dep['update_available']).lower()}")
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report-dir", default="dependency-update-report")
    parser.add_argument("--download-assets", action="store_true")
    parser.add_argument("--fail-on-update", action="store_true")
    args = parser.parse_args(argv)

    root = repo_root()
    report_dir = root / args.report_dir
    report_dir.mkdir(parents=True, exist_ok=True)

    report = {
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "repo_root": str(root),
        "dependencies": {},
    }

    update_available = False
    for name, dep in DEPS.items():
        result = analyze_dep(root, name, dep, report_dir, args.download_assets)
        report["dependencies"][name] = result
        update_available = update_available or result["update_available"]

    latest_json = report_dir / "latest.json"
    latest_json.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    write_markdown(report, report_dir / "DEPENDENCY_UPDATE_REVIEW.md")
    write_env(report, report_dir / "update.env")

    print(f"Wrote {latest_json}")
    print(f"Wrote {report_dir / 'DEPENDENCY_UPDATE_REVIEW.md'}")

    for name, dep in report["dependencies"].items():
        marker = "update available" if dep["update_available"] else "current"
        print(f"{name}: {dep['current_version']} -> {dep['latest_version']} ({marker})")
        if dep["missing_platforms"]:
            print(f"  missing platform assets: {', '.join(dep['missing_platforms'])}")
        for warning in dep["warnings"]:
            print(f"  warning: {warning}")

    if args.fail_on_update and update_available:
        return 10
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))

