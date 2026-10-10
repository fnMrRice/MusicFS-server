#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export CGO_ENABLED=0
target="${GOOS:-linux}"
export GOOS="$target"
suffix=""
if [ "$target" = windows ]; then suffix=.exe; fi
for name in musicfs musicfs-plugins; do
    go build -mod=readonly -trimpath -o "build/$target/$name$suffix" "./src/app/$name"
done
