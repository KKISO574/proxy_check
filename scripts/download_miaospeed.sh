#!/usr/bin/env bash
set -euo pipefail

release_latest_url="https://github.com/MiaoMagic/miaospeed/releases/latest"
download_base="https://github.com/MiaoMagic/miaospeed/releases/download"
default_output="runtime/bin/miaospeed"
user_agent="proxy-check-miaospeed-downloader/0.1"
download_connect_timeout="${DOWNLOAD_CONNECT_TIMEOUT:-10}"
download_max_time="${DOWNLOAD_MAX_TIME:-180}"

target_os=""
target_arch=""
version=""
output="$default_output"
print_url=false

usage() {
  cat <<'EOF'
Usage: scripts/download_miaospeed.sh [--os darwin|linux] [--arch arm64|amd64] [--version v4.3.9] [--output runtime/bin/miaospeed] [--print-url]

Download an official MiaoSpeed release binary into this project.
If --version is omitted, the script follows GitHub's latest-release redirect.
Set GITHUB_PROXY to prefix GitHub download URLs, for example https://proxy.example/.
Set DOWNLOAD_CONNECT_TIMEOUT and DOWNLOAD_MAX_TIME to override curl timeouts.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --os)
      target_os="${2:-}"
      shift 2
      ;;
    --arch)
      target_arch="${2:-}"
      shift 2
      ;;
    --version)
      version="${2:-}"
      shift 2
      ;;
    --output)
      output="${2:-}"
      shift 2
      ;;
    --print-url)
      print_url=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

detect_os() {
  case "$(uname -s | tr '[:upper:]' '[:lower:]')" in
    darwin) echo "darwin" ;;
    linux) echo "linux" ;;
    *)
      echo "unsupported OS: $(uname -s)" >&2
      exit 2
      ;;
  esac
}

detect_arch() {
  case "$(uname -m | tr '[:upper:]' '[:lower:]')" in
    arm64|aarch64) echo "arm64" ;;
    x86_64|amd64) echo "amd64" ;;
    *)
      echo "unsupported architecture: $(uname -m)" >&2
      exit 2
      ;;
  esac
}

tag_from_version() {
  case "$1" in
    v*) echo "$1" ;;
    *) echo "v$1" ;;
  esac
}

asset_version_from_tag() {
  case "$1" in
    v*) echo "${1#v}" ;;
    *) echo "$1" ;;
  esac
}

resolve_latest_version() {
  local final_url
  final_url="$(curl -fsSIL --connect-timeout "$download_connect_timeout" --max-time "$download_max_time" -A "$user_agent" -o /dev/null -w '%{url_effective}' "$release_latest_url")"
  case "$final_url" in
    */tag/*) echo "${final_url##*/tag/}" ;;
    *)
      echo "failed to resolve latest MiaoSpeed version from $final_url" >&2
      exit 1
      ;;
  esac
}

require_tool() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 2
  fi
}

download_url() {
  local url="$1"
  if [ -n "${GITHUB_PROXY:-}" ]; then
    printf '%s/%s\n' "${GITHUB_PROXY%/}" "$url"
  else
    printf '%s\n' "$url"
  fi
}

require_tool curl
require_tool tar

goos="${target_os:-$(detect_os)}"
arch="${target_arch:-$(detect_arch)}"

case "$goos" in
  darwin|linux) ;;
  *) echo "--os must be darwin or linux" >&2; exit 2 ;;
esac

case "$arch" in
  arm64|amd64) ;;
  *) echo "--arch must be arm64 or amd64" >&2; exit 2 ;;
esac

if [ -n "$version" ]; then
  tag="$(tag_from_version "$version")"
else
  echo "Resolving latest MiaoSpeed release for ${goos}/${arch}..."
  tag="$(resolve_latest_version)"
fi

asset_version="$(asset_version_from_tag "$tag")"
asset_name="miaospeed_${asset_version}_${goos}_${arch}.tar.gz"
asset_url="${download_base}/${tag}/${asset_name}"
effective_asset_url="$(download_url "$asset_url")"
download_dir="runtime/downloads"
archive_path="${download_dir}/${asset_name}"
tmp_archive="${archive_path}.tmp"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir" "$tmp_archive"
}
trap cleanup EXIT

mkdir -p "$download_dir" "$(dirname "$output")"

if [ "$print_url" = true ]; then
  echo "$effective_asset_url"
  exit 0
fi

echo "Downloading ${asset_name}..."
if ! curl -fsSL --connect-timeout "$download_connect_timeout" --max-time "$download_max_time" -A "$user_agent" -o "$tmp_archive" "$effective_asset_url"; then
  echo "failed to download MiaoSpeed asset: $effective_asset_url" >&2
  exit 1
fi

mv "$tmp_archive" "$archive_path"
tar -xzf "$archive_path" -C "$tmp_dir"

binary_path="$(find "$tmp_dir" -type f -name miaospeed -perm -u+x | head -n 1)"
if [ -z "$binary_path" ]; then
  binary_path="$(find "$tmp_dir" -type f -name miaospeed | head -n 1)"
fi

if [ -z "$binary_path" ]; then
  echo "downloaded archive does not contain a miaospeed binary" >&2
  exit 1
fi

cp "$binary_path" "$output"
chmod +x "$output"

echo "MiaoSpeed saved to ${output} from ${asset_name}"
