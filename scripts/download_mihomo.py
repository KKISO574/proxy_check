from __future__ import annotations

import argparse
import gzip
import json
import platform
import shutil
import stat
import sys
import urllib.request
from pathlib import Path

RELEASE_API = "https://api.github.com/repos/MetaCubeX/mihomo/releases/latest"
DOWNLOAD_BASE = "https://github.com/MetaCubeX/mihomo/releases/download"
DEFAULT_OUTPUT = Path("runtime/bin/mihomo")
USER_AGENT = "proxy-check-mihomo-downloader/0.1"


def detect_target() -> tuple[str, str]:
    system = platform.system().lower()
    machine = platform.machine().lower()
    if system == "darwin":
        goos = "darwin"
    elif system == "linux":
        goos = "linux"
    else:
        raise SystemExit(f"unsupported OS: {system}")

    if machine in {"arm64", "aarch64"}:
        arch = "arm64"
    elif machine in {"x86_64", "amd64"}:
        arch = "amd64"
    else:
        raise SystemExit(f"unsupported architecture: {machine}")
    return goos, arch


def find_asset(assets: list[dict[str, object]], goos: str, arch: str) -> dict[str, object]:
    candidates = []
    for asset in assets:
        name = str(asset.get("name", "")).lower()
        if not name.endswith(".gz"):
            continue
        if goos not in name or arch not in name:
            continue
        if "compatible" in name:
            return asset
        candidates.append(asset)
    if candidates:
        return candidates[0]
    raise SystemExit(f"no mihomo release asset found for {goos}/{arch}")


def version_tag(value: str) -> str:
    return value if value.startswith("v") else f"v{value}"


def candidate_names(goos: str, arch: str, version: str) -> list[str]:
    version = version_tag(version)
    names: list[str] = []
    if goos == "darwin" and arch == "arm64":
        names.extend(
            [
                f"mihomo-darwin-arm64-{version}.gz",
                f"mihomo-darwin-arm64-go124-{version}.gz",
                f"mihomo-darwin-arm64-go122-{version}.gz",
                f"mihomo-darwin-arm64-go120-{version}.gz",
            ]
        )
    elif goos == "darwin" and arch == "amd64":
        names.extend(
            [
                f"mihomo-darwin-amd64-compatible-{version}.gz",
                f"mihomo-darwin-amd64-{version}.gz",
                f"mihomo-darwin-amd64-v1-{version}.gz",
            ]
        )
    elif goos == "linux" and arch == "amd64":
        names.extend(
            [
                f"mihomo-linux-amd64-compatible-{version}.gz",
                f"mihomo-linux-amd64-{version}.gz",
                f"mihomo-linux-amd64-v1-{version}.gz",
                f"mihomo-linux-amd64-v2-{version}.gz",
                f"mihomo-linux-amd64-v3-{version}.gz",
            ]
        )
    elif goos == "linux" and arch == "arm64":
        names.append(f"mihomo-linux-arm64-{version}.gz")
    else:
        names.append(f"mihomo-{goos}-{arch}-{version}.gz")
    return names


def request_url(url: str, timeout: int = 60) -> urllib.request.Request:
    return urllib.request.Request(
        url,
        headers={
            "Accept": "application/octet-stream",
            "User-Agent": USER_AGENT,
        },
    )


def download(url: str, output: Path) -> None:
    output.parent.mkdir(parents=True, exist_ok=True)
    with urllib.request.urlopen(request_url(url), timeout=60) as response:
        output.write_bytes(response.read())


def fetch_latest_asset(goos: str, arch: str) -> tuple[str, str]:
    with urllib.request.urlopen(request_url(RELEASE_API, timeout=30), timeout=30) as response:
        release = json.loads(response.read().decode("utf-8"))
    asset = find_asset(release.get("assets", []), goos, arch)
    return str(asset["name"]), str(asset["browser_download_url"])


def fetch_versioned_asset(goos: str, arch: str, version: str) -> tuple[str, str]:
    version = version_tag(version)
    errors: list[str] = []
    for name in candidate_names(goos, arch, version):
        url = f"{DOWNLOAD_BASE}/{version}/{name}"
        try:
            with urllib.request.urlopen(request_url(url), timeout=20) as response:
                if response.status < 400:
                    return name, url
        except Exception as exc:
            errors.append(f"{name}: {exc}")
    raise SystemExit("no downloadable Mihomo asset found:\n" + "\n".join(errors))


def main() -> int:
    parser = argparse.ArgumentParser(description="Download Mihomo binary into this project.")
    parser.add_argument("--os", choices=["darwin", "linux"], help="target OS")
    parser.add_argument("--arch", choices=["arm64", "amd64"], help="target CPU architecture")
    parser.add_argument(
        "--version",
        help="release version, for example v1.19.25. Defaults to GitHub latest API.",
    )
    parser.add_argument("--output", default=str(DEFAULT_OUTPUT), help="output binary path")
    args = parser.parse_args()

    detected_os, detected_arch = detect_target()
    goos = args.os or detected_os
    arch = args.arch or detected_arch

    if args.version:
        asset_name, asset_url = fetch_versioned_asset(goos, arch, args.version)
    else:
        print(f"Fetching latest Mihomo release metadata for {goos}/{arch}...")
        asset_name, asset_url = fetch_latest_asset(goos, arch)

    gz_path = Path("runtime/downloads") / asset_name
    out_path = Path(args.output)

    print(f"Downloading {asset_name}...")
    download(asset_url, gz_path)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    with gzip.open(gz_path, "rb") as src, out_path.open("wb") as dst:
        shutil.copyfileobj(src, dst)
    out_path.chmod(out_path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
    print(f"Mihomo saved to {out_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
