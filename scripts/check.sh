#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mkdir -p build
set -- src tests
if [ -d scripts/release ]; then set -- "$@" scripts/release; fi
test -z "$(find "$@" -name '*.go' -exec gofmt -l {} +)"
find "$@" -name '*.go' -exec awk 'END { if (NR > 400) printf "Review source length (%d lines): %s\n", NR, FILENAME }' {} \;
set -- ./src/... ./tests/...
if [ -d scripts/release ]; then set -- "$@" ./scripts/release; fi
go vet -mod=readonly "$@"
go test -mod=readonly -count=1 -v -timeout="${TEST_TIMEOUT:-20m}" -coverpkg=./src/... -coverprofile=build/coverage.out ./tests/...
if [ "${RACE:-0}" = 1 ]; then go test -mod=readonly -count=1 -v -timeout="${TEST_TIMEOUT:-20m}" -race ./tests/...; fi
