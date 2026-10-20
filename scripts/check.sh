#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export GOWORK=off
mkdir -p build
set -- src tests sdk plugins
if [ -d scripts/release ]; then set -- "$@" scripts/release; fi
test -z "$(find "$@" -name '*.go' -exec gofmt -l {} +)"
find "$@" -name '*.go' -exec awk 'END { if (NR > 400) printf "Review source length (%d lines): %s\n", NR, FILENAME }' {} \;
set -- ./src/... ./plugins/... ./tests/...
if [ -d scripts/release ]; then set -- "$@" ./scripts/release; fi
go vet -mod=readonly "$@"
go test -mod=readonly -count=1 -v -timeout="${TEST_TIMEOUT:-20m}" -coverpkg=./src/...,./plugins/... -coverprofile=build/coverage.out ./tests/...
go test -mod=readonly -count=1 -timeout="${TEST_TIMEOUT:-20m}" ./src/... ./plugins/... ./scripts/release
for module in sdk plugins/source/online; do
    (
        cd "$module"
        go vet -mod=readonly ./...
        go test -mod=readonly -count=1 -timeout="${TEST_TIMEOUT:-20m}" ./...
        if [ "${RACE:-0}" = 1 ]; then go test -mod=readonly -count=1 -timeout="${TEST_TIMEOUT:-20m}" -race ./...; fi
    )
done
if [ "${RACE:-0}" = 1 ]; then go test -mod=readonly -count=1 -v -timeout="${TEST_TIMEOUT:-20m}" -race ./tests/...; fi
