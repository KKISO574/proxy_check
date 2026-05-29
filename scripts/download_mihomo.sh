#!/usr/bin/env bash
set -euo pipefail

release_latest_url="https://github.com/MetaCubeX/mihomo/releases/latest"
download_base="https://github.com/MetaCubeX/mihomo/releases/download"
default_output="runtime/bin/mihomo"
user_agent="proxy-check-mihomo-downloader/0.2"
download_connect_timeout="${DOWNLOAD_CONNECT_TIMEOUT:-10}"
download_max_time="${DOWNLOAD_MAX_TIME:-120}"
download_retry="${DOWNLOAD_RETRY:-3}"
download_retry_delay="${DOWNLOAD_RETRY_DELAY:-2}"

target_os=""
target_arch=""
version=""
output="$default_output"
print_url=false

usage() {
  cat <<'EOF'
Usage: scripts/download_mihomo.sh [--os darwin|linux] [--arch arm64|amd64] [--version v1.19.24] [--output runtime/bin/mihomo] [--print-url]

Download a Mihomo release binary into this project.
If --version is omitted, the script follows GitHub's latest-release redirect.
Set GITHUB_PROXY to prefix GitHub download URLs, for example https://proxy.example/.
Set DOWNLOAD_CONNECT_TIMEOUT and DOWNLOAD_MAX_TIME to override curl timeouts.
Set DOWNLOAD_RETRY and DOWNLOAD_RETRY_DELAY to override curl retry behavior.
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

version_tag() {
  case "$1" in
    v*) echo "$1" ;;
    *) echo "v$1" ;;
  esac
}

resolve_latest_version() {
  local final_url
  final_url="$(curl -fsSIL --retry "$download_retry" --retry-delay "$download_retry_delay" --retry-all-errors --connect-timeout "$download_connect_timeout" --max-time "$download_max_time" -A "$user_agent" -o /dev/null -w '%{url_effective}' "$release_latest_url")"
  case "$final_url" in
    */tag/*) echo "${final_url##*/tag/}" ;;
    *)
      echo "failed to resolve latest Mihomo version from $final_url" >&2
      exit 1
      ;;
  esac
}

candidate_names() {
  local goos="$1"
  local arch="$2"
  local tag="$3"

  if [ "$goos" = "darwin" ] && [ "$arch" = "arm64" ]; then
    printf '%s\n' \
      "mihomo-darwin-arm64-${tag}.gz" \
      "mihomo-darwin-arm64-go124-${tag}.gz" \
      "mihomo-darwin-arm64-go122-${tag}.gz" \
      "mihomo-darwin-arm64-go120-${tag}.gz"
  elif [ "$goos" = "darwin" ] && [ "$arch" = "amd64" ]; then
    printf '%s\n' \
      "mihomo-darwin-amd64-compatible-${tag}.gz" \
      "mihomo-darwin-amd64-${tag}.gz" \
      "mihomo-darwin-amd64-v1-${tag}.gz"
  elif [ "$goos" = "linux" ] && [ "$arch" = "amd64" ]; then
    printf '%s\n' \
      "mihomo-linux-amd64-compatible-${tag}.gz" \
      "mihomo-linux-amd64-${tag}.gz" \
      "mihomo-linux-amd64-v1-${tag}.gz" \
      "mihomo-linux-amd64-v2-${tag}.gz" \
      "mihomo-linux-amd64-v3-${tag}.gz"
  elif [ "$goos" = "linux" ] && [ "$arch" = "arm64" ]; then
    printf '%s\n' \
      "mihomo-linux-arm64-compatible-${tag}.gz" \
      "mihomo-linux-arm64-${tag}.gz"
  else
    printf '%s\n' "mihomo-${goos}-${arch}-${tag}.gz"
  fi
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
require_tool gzip

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
  tag="$(version_tag "$version")"
else
  echo "Resolving latest Mihomo release for ${goos}/${arch}..."
  tag="$(resolve_latest_version)"
fi

download_dir="runtime/downloads"
mkdir -p "$download_dir" "$(dirname "$output")"

selected_name=""
selected_gz=""

while IFS= read -r name; do
  [ -n "$name" ] || continue
  url="${download_base}/${tag}/${name}"
  effective_url="$(download_url "$url")"
  if [ "$print_url" = true ]; then
    echo "$effective_url"
    continue
  fi
  gz_path="${download_dir}/${name}"
  tmp_path="${gz_path}.tmp"

  echo "Trying ${name}..."
  if curl -fsSL --retry "$download_retry" --retry-delay "$download_retry_delay" --retry-all-errors --connect-timeout "$download_connect_timeout" --max-time "$download_max_time" -A "$user_agent" -o "$tmp_path" "$effective_url"; then
    mv "$tmp_path" "$gz_path"
    selected_name="$name"
    selected_gz="$gz_path"
    break
  fi
  rm -f "$tmp_path"
done <<EOF
$(candidate_names "$goos" "$arch" "$tag")
EOF

if [ "$print_url" = true ]; then
  exit 0
fi

if [ -z "$selected_gz" ]; then
  echo "no downloadable Mihomo asset found for ${goos}/${arch} ${tag}" >&2
  exit 1
fi

gzip -dc "$selected_gz" > "$output"
chmod +x "$output"

echo "Mihomo saved to ${output} from ${selected_name}"
