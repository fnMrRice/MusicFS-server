param([switch]$Race, [string]$TestTimeout = '20m')
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot)
New-Item -ItemType Directory -Force build | Out-Null
New-Item -ItemType Directory -Force build/test-tmp | Out-Null
$env:TMP = Join-Path (Get-Location) 'build/test-tmp'
$env:TEMP = $env:TMP
$sourceRoots = @('src', 'tests')
$vetPackages = @('./src/...', './tests/...')
if (Test-Path scripts/release) {
    $sourceRoots += 'scripts/release'
    $vetPackages += './scripts/release'
}
$unformatted = & gofmt -l @sourceRoots
if ($LASTEXITCODE) { throw 'gofmt check failed' }
if ($unformatted) { throw "Unformatted Go files: $unformatted" }
Get-ChildItem $sourceRoots -Recurse -Filter '*.go' | ForEach-Object {
    $lines = [System.IO.File]::ReadAllLines($_.FullName).Length
    if ($lines -gt 400) { Write-Warning "Review source length ($lines lines): $($_.FullName)" }
}
go vet -mod=readonly @vetPackages
if ($LASTEXITCODE) { throw 'go vet failed' }
go test -mod=readonly -count=1 -v "-timeout=$TestTimeout" '-coverpkg=./src/...' '-coverprofile=build/coverage.out' ./tests/...
if ($LASTEXITCODE) { throw 'go test failed' }
if ($Race) {
    go test -mod=readonly -count=1 -v "-timeout=$TestTimeout" -race ./tests/...
    if ($LASTEXITCODE) { throw 'race check failed' }
}
