param([ValidateSet('windows','linux')][string]$TargetOS='windows', [Parameter(Mandatory=$true)][string]$Output, [string]$WebRoot='', [switch]$AllowDirty)
$ErrorActionPreference='Stop'
Set-Location (Split-Path $PSScriptRoot)
$arguments=@('run','-mod=readonly','./scripts/release','--os',$TargetOS,'--output',$Output)
if ($WebRoot) { $arguments+=@('--web-root',$WebRoot) }
if ($AllowDirty) { $arguments+='--allow-dirty' }
& go @arguments
if ($LASTEXITCODE) { throw 'Release build failed' }
