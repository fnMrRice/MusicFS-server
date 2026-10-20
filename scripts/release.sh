#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export GOWORK=off
exec go run -mod=readonly ./scripts/release "$@"
