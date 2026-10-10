param([switch]$Race)
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot)
New-Item -ItemType Directory -Force build | Out-Null
New-Item -ItemType Directory -Force build/test-tmp | Out-Null
$env:TMP = Join-Path (Get-Location) 'build/test-tmp'
$env:TEMP = $env:TMP
$unformatted = & gofmt -l src tests
if ($LASTEXITCODE) { throw 'gofmt check failed' }
if ($unformatted) { throw "Unformatted Go files: $unformatted" }
go vet -mod=readonly ./src/... ./tests/...
if ($LASTEXITCODE) { throw 'go vet failed' }
go test -mod=readonly '-coverpkg=./src/...' '-coverprofile=build/coverage.out' ./tests/...
if ($LASTEXITCODE) { throw 'go test failed' }
if ($Race) {
    go test -mod=readonly -race ./tests/...
    if ($LASTEXITCODE) { throw 'race check failed' }
}
