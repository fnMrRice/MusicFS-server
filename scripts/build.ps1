param([string]$TargetOS = 'windows')
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot)
$env:GOOS = $TargetOS
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
$suffix = if ($TargetOS -eq 'windows') { '.exe' } else { '' }
Get-ChildItem src/app -Directory | ForEach-Object {
    go build -mod=readonly -trimpath -o "build/$TargetOS/$($_.Name)$suffix" "./src/app/$($_.Name)"
    if ($LASTEXITCODE) { throw "Build failed: $($_.Name)" }
}
