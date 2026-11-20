param([switch]$Race, [string]$TestTimeout = '20m')
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot)
$env:GOWORK = 'off'
$misplacedTests = @(Get-ChildItem src -Recurse -File -Filter '*_test.go')
if ($misplacedTests) {
    throw "Host tests must be under tests/: $($misplacedTests.FullName -join ', ')"
}
New-Item -ItemType Directory -Force build | Out-Null
New-Item -ItemType Directory -Force build/test-tmp | Out-Null
$env:TMP = Join-Path (Get-Location) 'build/test-tmp'
$env:TEMP = $env:TMP
$sourceRoots = @('src', 'tests', 'sdk', 'plugins')
$vetPackages = @('./src/...', './plugins/...', './tests/...')
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
go test -mod=readonly -count=1 -v "-timeout=$TestTimeout" '-coverpkg=./src/...,./plugins/...' '-coverprofile=build/coverage.out' ./tests/...
if ($LASTEXITCODE) { throw 'go test failed' }
go test -mod=readonly -count=1 "-timeout=$TestTimeout" ./src/... ./plugins/... ./scripts/release ./scripts/internal/pluginversions
if ($LASTEXITCODE) { throw 'package tests failed' }
foreach ($module in @('sdk', 'plugins/source/online')) {
    Push-Location $module
    try {
        go vet -mod=readonly ./...
        if ($LASTEXITCODE) { throw "go vet failed: $module" }
        go test -mod=readonly -count=1 "-timeout=$TestTimeout" ./...
        if ($LASTEXITCODE) { throw "go test failed: $module" }
        if ($Race) {
            go test -mod=readonly -count=1 "-timeout=$TestTimeout" -race ./...
            if ($LASTEXITCODE) { throw "race check failed: $module" }
        }
    } finally {
        Pop-Location
    }
}
if ($Race) {
    go test -mod=readonly -count=1 -v "-timeout=$TestTimeout" -race ./tests/...
    if ($LASTEXITCODE) { throw 'race check failed' }
}
