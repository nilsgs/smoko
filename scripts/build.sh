#!/usr/bin/env sh
set -eu

repo_dir="$(cd "$(dirname "$0")/.." && pwd)"
src_dir="$repo_dir/src"
dist_dir="$repo_dir/dist"
version="$(tr -d '\r\n' < "$repo_dir/VERSION")"
commit="$(git -C "$repo_dir" rev-parse --short HEAD 2>/dev/null || printf 'unknown')"
ldflags="-s -w -X main.version=${version} -X main.commit=${commit}"

echo "Building smoko v${version}+${commit}..."
mkdir -p "$dist_dir"

cd "$src_dir"
go build -ldflags "$ldflags" -o "$dist_dir/smoko" ./cmd/smoko
