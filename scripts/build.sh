#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export CGO_ENABLED=0
target="${GOOS:-linux}"
export GOOS="$target"
suffix=""
if [ "$target" = windows ]; then suffix=.exe; fi
for entry in src/app/*; do
    name=$(basename "$entry")
    go build -mod=readonly -trimpath -o "build/$target/$name$suffix" "./$entry"
done
