#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mkdir -p build
test -z "$(find src tests -name '*.go' -exec gofmt -l {} +)"
go vet -mod=readonly ./src/... ./tests/...
go test -mod=readonly -coverpkg=./src/... -coverprofile=build/coverage.out ./tests/...
if [ "${RACE:-0}" = 1 ]; then go test -mod=readonly -race ./tests/...; fi
