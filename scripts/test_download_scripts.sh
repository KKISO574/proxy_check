#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"

assert_contains() {
  local haystack="$1"
  local needle="$2"
  if [[ "$haystack" != *"$needle"* ]]; then
    printf 'expected output to contain %q, got:\n%s\n' "$needle" "$haystack" >&2
    exit 1
  fi
}

miaospeed_url="$(
  GITHUB_PROXY="https://proxy.example" \
    "$repo_root/scripts/download_miaospeed.sh" --os linux --arch amd64 --version v4.3.9 --print-url
)"
assert_contains "$miaospeed_url" "https://proxy.example/https://github.com/MiaoMagic/miaospeed/releases/download/v4.3.9/miaospeed_4.3.9_linux_amd64.tar.gz"

mihomo_urls="$(
  GITHUB_PROXY="https://proxy.example/" \
    "$repo_root/scripts/download_mihomo.sh" --os linux --arch amd64 --version v1.19.24 --print-url
)"
assert_contains "$mihomo_urls" "https://proxy.example/https://github.com/MetaCubeX/mihomo/releases/download/v1.19.24/mihomo-linux-amd64-compatible-v1.19.24.gz"
assert_contains "$mihomo_urls" "https://proxy.example/https://github.com/MetaCubeX/mihomo/releases/download/v1.19.24/mihomo-linux-amd64-v3-v1.19.24.gz"

miaospeed_help="$("$repo_root/scripts/download_miaospeed.sh" --help)"
assert_contains "$miaospeed_help" "DOWNLOAD_CONNECT_TIMEOUT"
assert_contains "$miaospeed_help" "DOWNLOAD_MAX_TIME"

mihomo_help="$("$repo_root/scripts/download_mihomo.sh" --help)"
assert_contains "$mihomo_help" "DOWNLOAD_CONNECT_TIMEOUT"
assert_contains "$mihomo_help" "DOWNLOAD_MAX_TIME"

echo "download script URL tests passed"
