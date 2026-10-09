param([string]$BaseURL = 'http://127.0.0.1:8080', [string]$ProviderID = '')
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
    $headers['Idempotency-Key'] = [guid]::NewGuid().ToString()
    $provider = [uri]::EscapeDataString($ProviderID)
    $accepted = Invoke-RestMethod "$BaseURL/api/v1/provider-instances/$provider/scan-jobs" -Method Post -ContentType 'application/json' -Body '{"mode":"full"}' -Headers $headers -WebSession $session -TimeoutSec 15
    $deadline = [datetime]::UtcNow.AddMinutes(10)
    do {
        $job = Invoke-RestMethod "$BaseURL/api/v1/scan-jobs/$($accepted.jobId)" -Headers $headers -WebSession $session -TimeoutSec 15
        if ($job.status -notin @('queued', 'running')) { break }
        if ([datetime]::UtcNow -ge $deadline) { throw 'Scan wait timed out; query the job before retrying' }
        Start-Sleep -Milliseconds 200
    } while ($true)
    if ($job.status -ne 'succeeded') { throw "Scan ended with status $($job.status)" }
    $job | ConvertTo-Json -Depth 5
} finally {
    Invoke-RestMethod "$BaseURL/api/v1/auth/session" -Method Delete -Headers $headers -WebSession $session -TimeoutSec 15 | Out-Null
}
