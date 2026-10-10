param([string]$TargetOS = 'windows')
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot)
$env:GOOS = $TargetOS
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
$suffix = if ($TargetOS -eq 'windows') { '.exe' } else { '' }
$programs = @('musicfs', 'musicfs-plugin-local', 'musicfs-plugin-audio-ffmpeg', 'musicfs-plugin-ncm', 'musicfs-plugin-scraper-local')
foreach ($program in $programs) {
    go build -mod=readonly -trimpath -o "build/$TargetOS/$program$suffix" "./src/app/$program"
    if ($LASTEXITCODE) { throw "Build failed: $program" }
}
