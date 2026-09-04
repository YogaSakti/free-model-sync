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
CGO_ENABLED=1 go build -trimpath -buildmode=c-shared \
  -ldflags "-s -w -X main.Version=$version" \
  -o "$output" .
rm -f "${output%.*}.h"
printf '%s\n' "$output"
