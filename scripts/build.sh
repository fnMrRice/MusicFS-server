#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export CGO_ENABLED=0
export GOWORK=off
target="${GOOS:-linux}"
export GOOS="$target"
suffix=""
if [ "$target" = windows ]; then suffix=.exe; fi
for entry in \
    musicfs:./src/app/musicfs \
    musicfs-plugins:./src/app/musicfs-plugins \
    musicfs-plugin-storage-sqlite:./plugins/database/sqlite/cmd/musicfs-plugin-storage-sqlite \
    musicfs-plugin-dsp-gain:./plugins/dsp/gain/cmd/musicfs-plugin-dsp-gain; do
    name="${entry%%:*}"
    package_path="${entry#*:}"
    go build -mod=readonly -trimpath -o "build/$target/$name$suffix" "$package_path"
done
output_root="$(pwd)/build/$target"
(
    cd plugins/source/online
    for name in musicfs-plugin-netease musicfs-plugin-qqmusic musicfs-plugin-applemusic musicfs-plugin-online; do
        go build -mod=readonly -trimpath -o "$output_root/$name$suffix" "./cmd/$name"
    done
)
