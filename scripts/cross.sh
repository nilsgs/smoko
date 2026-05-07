#!/usr/bin/env sh
set -eu

repo_dir="$(cd "$(dirname "$0")/.." && pwd)"
src_dir="$repo_dir/src"
dist_dir="$repo_dir/dist"
version="$(tr -d '\r\n' < "$repo_dir/VERSION")"
commit="$(git -C "$repo_dir" rev-parse --short HEAD 2>/dev/null || printf 'unknown')"
ldflags="-s -w -X main.version=${version} -X main.commit=${commit}"

echo "Cross-building smoko v${version}+${commit}..."
mkdir -p "$dist_dir"

cd "$src_dir"
for spec in \
  "linux amd64 smoko-linux-amd64" \
  "linux arm64 smoko-linux-arm64" \
  "darwin amd64 smoko-darwin-amd64" \
  "darwin arm64 smoko-darwin-arm64" \
  "windows amd64 smoko-windows-amd64.exe" \
  "windows arm64 smoko-windows-arm64.exe"
do
  set -- $spec
  GOOS="$1" GOARCH="$2" go build -ldflags "$ldflags" -o "$dist_dir/$3" ./cmd/smoko
done
