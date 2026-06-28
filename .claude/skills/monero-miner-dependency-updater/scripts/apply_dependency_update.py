#!/usr/bin/env python3
"""Apply pinned XMRig/P2Pool versions and SHA256 sums from latest.json."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path


def repo_root() -> Path:
    return Path.cwd()


def template_asset_name(dep: dict, asset_name: str) -> str:
    version = dep["latest_version"]
    var = dep["version_var"]
    templated = asset_name.replace("v" + version, "v${" + var + "}")
    templated = templated.replace(version, "${" + var + "}")
    return templated


def usable_assets(dep: dict) -> dict[str, dict]:
    assets = {}
    for platform, asset in dep["selected_assets"].items():
        if not asset.get("present"):
            raise RuntimeError(f"{dep['name']}: missing selected asset for {platform}")
        if not asset.get("sha256"):
            raise RuntimeError(f"{dep['name']}: missing SHA256 for {platform} asset {asset.get('name')}")
        assets[platform] = asset
    return assets


def update_script(path: Path, dep: dict, dry_run: bool) -> bool:
    original = path.read_text(encoding="utf-8")
    text = original

    var = dep["version_var"]
    latest = dep["latest_version"]
    text = re.sub(r'^{}="[^"]+"'.format(re.escape(var)), f'{var}="{latest}"', text, flags=re.MULTILINE)
    text = re.sub(r"SHA256 checksums for v[0-9A-Za-z_.-]+", f"SHA256 checksums for v{latest}", text)

    assets = usable_assets(dep)
    out_lines = []
    in_urls = False
    in_sums = False

    for line in text.splitlines(keepends=True):
        stripped = line.strip()
        if stripped.startswith("declare -A URLS=("):
            in_urls = True
            in_sums = False
            out_lines.append(line)
            continue
        if stripped.startswith("declare -A SUMS=("):
            in_sums = True
            in_urls = False
            out_lines.append(line)
            continue
        if stripped == ")":
            in_urls = False
            in_sums = False
            out_lines.append(line)
            continue

        updated = line
        if in_urls:
            for platform, asset in assets.items():
                if stripped.startswith(f'["{platform}"]='):
                    filename = template_asset_name(dep, asset["name"])
                    updated = f'  ["{platform}"]="${{GITHUB}}/{filename}"\n'
                    break
        elif in_sums:
            for platform, asset in assets.items():
                if stripped.startswith(f'["{platform}"]='):
                    updated = f'  ["{platform}"]="{asset["sha256"]}"\n'
                    break
        out_lines.append(updated)

    text = "".join(out_lines)
    changed = text != original
    if changed and not dry_run:
        path.write_text(text, encoding="utf-8")
    return changed


def update_deps_json(path: Path, report: dict, dry_run: bool) -> bool:
    """Update the embedded runtime pin manifest (internal/autoinstall/deps.json).

    This is the source of truth for the runtime autoinstall path, which verifies
    each download against the pinned SHA256. Only platforms with a present asset
    and a known hash are written, so a dropped/renamed upstream platform is
    omitted rather than pinned to a stale entry.
    """
    data = json.loads(path.read_text(encoding="utf-8"))
    changed = False
    for name, dep in report["dependencies"].items():
        if not dep.get("update_available") or name not in data:
            continue
        new_assets = {}
        for platform, asset in dep["selected_assets"].items():
            if not asset.get("present") or not asset.get("sha256"):
                continue
            new_assets[platform] = {"file": asset["name"], "sha256": asset["sha256"]}
        if not new_assets:
            continue
        data[name]["version"] = dep["latest_version"]
        data[name]["assets"] = new_assets
        changed = True
    if changed and not dry_run:
        path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    return changed


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("latest_json", help="Path to dependency-update-report/latest.json")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args(argv)

    report = json.loads(Path(args.latest_json).read_text(encoding="utf-8"))
    root = repo_root()
    changed = []

    for dep in report["dependencies"].values():
        if not dep.get("update_available"):
            continue
        script = root / dep["script"]
        if update_script(script, dep, args.dry_run):
            changed.append(str(script))

    deps_path = root / "internal" / "autoinstall" / "deps.json"
    if deps_path.is_file() and update_deps_json(deps_path, report, args.dry_run):
        changed.append(str(deps_path))

    if changed:
        verb = "Would update" if args.dry_run else "Updated"
        for path in changed:
            print(f"{verb}: {path}")
    else:
        print("No dependency script changes were needed.")

    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(1)

