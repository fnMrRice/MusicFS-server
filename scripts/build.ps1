param([string]$TargetOS = 'windows')
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot)
$env:GOOS = $TargetOS
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
$env:GOWORK = 'off'
$suffix = if ($TargetOS -eq 'windows') { '.exe' } else { '' }
$programs = [ordered]@{
    'musicfs' = './src/app/musicfs'
    'musicfs-plugins' = './src/app/musicfs-plugins'
    'musicfs-plugin-storage-sqlite' = './plugins/database/sqlite/cmd/musicfs-plugin-storage-sqlite'
    'musicfs-plugin-dsp-gain' = './plugins/dsp/gain/cmd/musicfs-plugin-dsp-gain'
}
foreach ($program in $programs.Keys) {
    $packagePath = $programs[$program]
    go build -mod=readonly -trimpath -o "build/$TargetOS/$program$suffix" $packagePath
    if ($LASTEXITCODE) { throw "Build failed: $program" }
}
$outputRoot = (Resolve-Path "build/$TargetOS").Path
Push-Location plugins/source/online
try {
    foreach ($program in @('musicfs-plugin-netease', 'musicfs-plugin-qqmusic', 'musicfs-plugin-applemusic', 'musicfs-plugin-online')) {
        go build -mod=readonly -trimpath -o "$outputRoot/$program$suffix" "./cmd/$program"
        if ($LASTEXITCODE) { throw "Build failed: $program" }
    }
} finally {
    Pop-Location
}
