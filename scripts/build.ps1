param([ValidateSet('windows', 'linux')][string]$TargetOS = 'windows', [switch]$SplitPlugins)
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot)
$env:GOOS = $TargetOS
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
$env:GOWORK = 'off'
$suffix = if ($TargetOS -eq 'windows') { '.exe' } else { '' }
$repositoryRoot = (Get-Location).Path
$outputRoot = [System.IO.Path]::GetFullPath((Join-Path $repositoryRoot "build/$TargetOS"))
foreach ($directory in @((Join-Path $repositoryRoot 'build'), $outputRoot)) {
    if ((Test-Path -LiteralPath $directory) -and ((Get-Item -LiteralPath $directory).Attributes -band [System.IO.FileAttributes]::ReparsePoint)) {
        throw "Build output must not be a link: $directory"
    }
}
$splitNames = @('musicfs-plugin-storage-sqlite', 'musicfs-plugin-dsp-gain', 'musicfs-plugin-netease', 'musicfs-plugin-qqmusic', 'musicfs-plugin-applemusic', 'musicfs-plugin-online')
$programs = [ordered]@{
    'musicfs' = './src/app/musicfs'
    'musicfs-plugins' = './src/app/musicfs-plugins'
}
if ($SplitPlugins) {
    $programs['musicfs-plugin-storage-sqlite'] = './plugins/database/sqlite/cmd/musicfs-plugin-storage-sqlite'
    $programs['musicfs-plugin-dsp-gain'] = './plugins/dsp/gain/cmd/musicfs-plugin-dsp-gain'
}
foreach ($program in $programs.Keys) {
    $packagePath = $programs[$program]
    go build -mod=readonly -trimpath -o "build/$TargetOS/$program$suffix" $packagePath
    if ($LASTEXITCODE) { throw "Build failed: $program" }
}
$obsoleteNames = @('musicfs-plugin-local', 'musicfs-plugin-audio-ffmpeg', 'musicfs-plugin-ncm', 'musicfs-plugin-scraper-local')
if (-not $SplitPlugins) { $obsoleteNames += $splitNames }
foreach ($program in $obsoleteNames) {
        $obsoletePath = [System.IO.Path]::GetFullPath((Join-Path $outputRoot "$program$suffix"))
        if ([System.IO.Path]::GetDirectoryName($obsoletePath) -ne $outputRoot) { throw 'Invalid generated file path' }
        if (Test-Path -LiteralPath $obsoletePath) {
            $item = Get-Item -LiteralPath $obsoletePath
            if (-not $item.PSIsContainer -and -not ($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint)) {
                Remove-Item -LiteralPath $obsoletePath -Force
            }
        }
}
if (-not $SplitPlugins) {
    return
}
Push-Location plugins/source/online
try {
    foreach ($program in @('musicfs-plugin-netease', 'musicfs-plugin-qqmusic', 'musicfs-plugin-applemusic', 'musicfs-plugin-online')) {
        go build -mod=readonly -trimpath -o "$outputRoot/$program$suffix" "./cmd/$program"
        if ($LASTEXITCODE) { throw "Build failed: $program" }
    }
} finally {
    Pop-Location
}
