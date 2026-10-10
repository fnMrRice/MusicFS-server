param(
    [string]$BaseURL = 'http://127.0.0.1:8080',
    [string]$ProviderID = '',
    [ValidateSet('full', 'incremental')][string]$Mode = 'full',
    [string[]]$RootIDs,
    [bool]$Recursive,
    [ValidateRange(0, 64)][int]$MaxDepth,
    [bool]$ImportLyrics
)
$ErrorActionPreference = 'Stop'
$BaseURL = $BaseURL.TrimEnd('/')
if (-not $env:MUSICFS_ADMIN_TOKEN) { throw 'Set MUSICFS_ADMIN_TOKEN first' }
$session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$headers = @{ Origin = $BaseURL; Authorization = "Bearer $env:MUSICFS_ADMIN_TOKEN" }
$login = Invoke-RestMethod "$BaseURL/api/v1/auth/session" -Method Post -Headers $headers -WebSession $session -TimeoutSec 15
$headers.Remove('Authorization')
$headers['X-CSRF-Token'] = $login.csrfToken
try {
    $providers = (Invoke-RestMethod "$BaseURL/api/v1/provider-instances" -Headers $headers -WebSession $session -TimeoutSec 15).items
    if (-not $ProviderID) {
        if ($providers.Count -ne 1) { throw 'Specify ProviderID when zero or multiple providers exist' }
        $ProviderID = $providers[0].id
    }
    $selected = @($providers | Where-Object { $_.id -ceq $ProviderID })
    if ($selected.Count -ne 1) { throw 'ProviderID must identify an authorized provider' }
    $selected = $selected[0]
    if (-not $selected.configuration.schemaRevision) { throw 'Provider does not declare import configuration' }
    if (-not $PSBoundParameters.ContainsKey('RootIDs')) {
        $RootIDs = @($selected.roots | ForEach-Object { $_.id })
    }
    if ($RootIDs.Count -eq 0) { throw 'Select at least one authorized root ID' }
    $options = @{}
    if ($PSBoundParameters.ContainsKey('Recursive')) { $options.recursive = $Recursive }
    if ($PSBoundParameters.ContainsKey('MaxDepth')) { $options.maxDepth = $MaxDepth }
    if ($PSBoundParameters.ContainsKey('ImportLyrics')) { $options.importLyrics = $ImportLyrics }
    $body = @{
        mode = $Mode
        schemaRevision = $selected.configuration.schemaRevision
        rootIds = @($RootIDs)
        options = $options
    } | ConvertTo-Json -Depth 10 -Compress
    $headers['Idempotency-Key'] = [guid]::NewGuid().ToString()
    $provider = [uri]::EscapeDataString($ProviderID)
    $accepted = Invoke-RestMethod "$BaseURL/api/v1/provider-instances/$provider/scan-jobs" -Method Post -ContentType 'application/json' -Body $body -Headers $headers -WebSession $session -TimeoutSec 15
    $deadline = [datetime]::UtcNow.AddHours(1)
    do {
        $job = Invoke-RestMethod "$BaseURL/api/v1/scan-jobs/$($accepted.jobId)" -Headers $headers -WebSession $session -TimeoutSec 15
        if ($job.status -notin @('queued', 'running')) { break }
        if ([datetime]::UtcNow -ge $deadline) { throw "Scan wait timed out for job $($accepted.jobId); query the job before retrying" }
        Start-Sleep -Milliseconds 200
    } while ($true)
    if ($job.status -ne 'succeeded') { throw "Scan ended with status $($job.status)" }
    $job | ConvertTo-Json -Depth 5
} finally {
    Invoke-RestMethod "$BaseURL/api/v1/auth/session" -Method Delete -Headers $headers -WebSession $session -TimeoutSec 15 | Out-Null
}
