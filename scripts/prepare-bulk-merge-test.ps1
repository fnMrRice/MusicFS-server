param([string]$OutputRoot = '')
$ErrorActionPreference = 'Stop'
$repository = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if (-not $OutputRoot) { $OutputRoot = Join-Path $repository '.cache' }
$output = [IO.Path]::GetFullPath($OutputRoot)
if (-not $output.StartsWith($repository + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Test snapshots must stay inside the server repository.'
}
$snapshot = Join-Path $output ('bulk-merge-' + [guid]::NewGuid().ToString('N'))
$archive = $snapshot + '.zip'
New-Item -ItemType Directory -Path $snapshot -Force | Out-Null
# Export the committed base so unrelated in-progress changes do not block verification.
git -C $repository archive --format=zip -o $archive HEAD
if ($LASTEXITCODE -ne 0) { throw 'Could not export the server baseline.' }
Expand-Archive -LiteralPath $archive -DestinationPath $snapshot
$files = @(
    'src/metadata/matching/bulk_merge.go',
    'src/metadata/matching/repository.go',
    'src/app/api/matching.go',
    'src/app/api/matching_bulk_merge.go',
    'plugins/database/sqlite/matching_transactions.go',
    'plugins/database/sqlite/matching_bulk_graph.go',
    'plugins/database/sqlite/matching_bulk_merge.go',
    'src/storage/rpcbackend/client_00.go',
    'src/storage/rpcbackend/client_01.go',
    'src/storage/rpcbackend/client_02.go',
    'src/storage/rpcbackend/client_03.go',
    'src/storage/rpcbackend/client_04.go',
    'src/storage/rpcbackend/client_05.go',
    'src/storage/rpcbackend/client_06.go',
    'src/storage/rpcbackend/client_07.go',
    'docs/contracts/http.schema.json',
    'docs/contracts/openapi.yaml',
    'tests/testdata/contracts/api/matching.json'
)
$tests = Get-ChildItem -LiteralPath (Join-Path $repository 'tests/integration/storage'), (Join-Path $repository 'tests/contract/api') -Filter '*bulk*test.go'
foreach ($testFile in $tests) { $files += [IO.Path]::GetRelativePath($repository, $testFile.FullName) }
foreach ($file in $files) {
    $source = Join-Path $repository $file
    $target = Join-Path $snapshot $file
    New-Item -ItemType Directory -Path ([IO.Path]::GetDirectoryName($target)) -Force | Out-Null
    Copy-Item -LiteralPath $source -Destination $target
}
Write-Output $snapshot
