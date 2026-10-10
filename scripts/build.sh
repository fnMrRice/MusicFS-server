#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export CGO_ENABLED=0
target="${GOOS:-linux}"
export GOOS="$target"
suffix=""
if [ "$target" = windows ]; then suffix=.exe; fi
for name in musicfs musicfs-plugin-local musicfs-plugin-audio-ffmpeg musicfs-plugin-ncm musicfs-plugin-scraper-local musicfs-plugin-netease musicfs-plugin-qqmusic musicfs-plugin-applemusic; do
    go build -mod=readonly -trimpath -o "build/$target/$name$suffix" "./src/app/$name"
done
