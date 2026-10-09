param([switch]$Race)
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot)
New-Item -ItemType Directory -Force build | Out-Null
$files = Get-ChildItem src,tests -Filter *.go -Recurse | ForEach-Object FullName
$unformatted = & gofmt -l @files
if ($unformatted) { throw "Unformatted Go files: $unformatted" }
go vet ./...
if ($LASTEXITCODE) { throw 'go vet failed' }
go test -coverpkg=./src/... -coverprofile=build/coverage.out ./tests/...
if ($LASTEXITCODE) { throw 'go test failed' }
if ($Race) {
    go test -race ./tests/...
    if ($LASTEXITCODE) { throw 'race check failed' }
}
