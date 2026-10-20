#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export CGO_ENABLED=0
export GOWORK=off
target="${GOOS:-linux}"
case "$target" in windows|linux) ;; *) echo "Target OS must be windows or linux" >&2; exit 1 ;; esac
split_plugins=false
case "${1:-}" in
    --split-plugins) split_plugins=true; shift ;;
    '') ;;
    *) echo "Usage: build.sh [--split-plugins]" >&2; exit 1 ;;
esac
if [ "$#" -ne 0 ]; then echo "Usage: build.sh [--split-plugins]" >&2; exit 1; fi
export GOOS="$target"
suffix=""
if [ "$target" = windows ]; then suffix=.exe; fi
if [ -L build ] || [ -L "build/$target" ]; then
    echo "Build output must not be a link" >&2
    exit 1
fi
output_root="$(pwd -P)/build/$target"
for entry in \
    musicfs:./src/app/musicfs \
    musicfs-plugins:./src/app/musicfs-plugins; do
    name="${entry%%:*}"
    package_path="${entry#*:}"
    go build -mod=readonly -trimpath -o "$output_root/$name$suffix" "$package_path"
done
obsolete_names="musicfs-plugin-local musicfs-plugin-audio-ffmpeg musicfs-plugin-ncm musicfs-plugin-scraper-local"
if [ "$split_plugins" = false ]; then
    obsolete_names="$obsolete_names musicfs-plugin-storage-sqlite musicfs-plugin-dsp-gain musicfs-plugin-netease musicfs-plugin-qqmusic musicfs-plugin-applemusic musicfs-plugin-online"
fi
for name in $obsolete_names; do
    obsolete="$output_root/$name$suffix"
    if [ -f "$obsolete" ] && [ ! -L "$obsolete" ]; then rm -f -- "$obsolete"; fi
done
if [ "$split_plugins" = false ]; then
    exit 0
fi
for entry in \
    musicfs-plugin-storage-sqlite:./plugins/database/sqlite/cmd/musicfs-plugin-storage-sqlite \
    musicfs-plugin-dsp-gain:./plugins/dsp/gain/cmd/musicfs-plugin-dsp-gain; do
    name="${entry%%:*}"
    package_path="${entry#*:}"
    go build -mod=readonly -trimpath -o "build/$target/$name$suffix" "$package_path"
done
(
    cd plugins/source/online
    for name in musicfs-plugin-netease musicfs-plugin-qqmusic musicfs-plugin-applemusic musicfs-plugin-online; do
        go build -mod=readonly -trimpath -o "$output_root/$name$suffix" "./cmd/$name"
    done
)
