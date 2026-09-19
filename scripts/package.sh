#!/bin/sh
set -eu

version=${1:?usage: package.sh VERSION LIBRARY [OUTPUT_DIR]}
library=${2:?usage: package.sh VERSION LIBRARY [OUTPUT_DIR]}
out_dir=${3:-./dist}

case "$(basename "$library")" in
  *.dylib) goos=darwin; ext=dylib ;;
  *.so) goos=linux; ext=so ;;
  *.dll) goos=windows; ext=dll ;;
  *) echo "unsupported library extension" >&2; exit 1 ;;
esac

# A numeric version is published as v<version>; a label like "dev" is used as is.
case "$version" in
  [0-9]*) label="v$version" ;;
  *) label="$version" ;;
esac

goarch=$(go env GOARCH)
asset="free-model-sync_${version}_${goos}_${goarch}.zip"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$out_dir"
cp "$library" "$stage/free-model-sync-$label.$ext"
archive=$(cd "$out_dir" && pwd)/$asset
(cd "$stage" && zip -q "$archive" "free-model-sync-$label.$ext")
(cd "$out_dir" && shasum -a 256 "$asset" > checksums.txt)
printf '%s\n' "$out_dir/$asset" "$out_dir/checksums.txt"
