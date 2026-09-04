#!/bin/sh
set -eu

version=${1:-dev}
out_dir=${2:-./dist}
mkdir -p "$out_dir"

case "$(go env GOOS)" in
  darwin) ext=dylib ;;
  windows) ext=dll ;;
  *) ext=so ;;
esac

output="$out_dir/free-model-sync.$ext"
commit=$(git rev-parse --short HEAD 2>/dev/null || printf '%s' none)
build_date=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
CGO_ENABLED=1 go build -trimpath -buildmode=c-shared \
  -ldflags "-s -w -X main.Version=$version -X main.Commit=$commit -X main.BuildDate=$build_date" \
  -o "$output" .
rm -f "${output%.*}.h"
printf '%s\n' "$output"
