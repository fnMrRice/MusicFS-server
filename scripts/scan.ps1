param(
    [string]$BaseURL = 'http://127.0.0.1:8080',
    [string]$ProviderID = '',
    [ValidateSet('full', 'incremental')][string]$Mode = 'full',
    [string[]]$RootIDs,
    [bool]$Recursive,
    [ValidateRange(0, 64)][int]$MaxDepth,
    [bool]$ImportLyrics,
    [AllowEmptyCollection()][string[]]$ExcludePatterns
)
$ErrorActionPreference = 'Stop'

function Send-ControlMessage($Socket, $Message, $Token) {
    $bytes = [Text.Encoding]::UTF8.GetBytes(($Message | ConvertTo-Json -Compress))
    $segment = [ArraySegment[byte]]::new($bytes)
    [void]$Socket.SendAsync($segment, [Net.WebSockets.WebSocketMessageType]::Text, $true, $Token).GetAwaiter().GetResult()
}

function Read-ControlMessage($Socket, $Connection, $Heartbeat) {
    $buffer = [byte[]]::new(65536)
    $used = 0
    do {
        $segment = [ArraySegment[byte]]::new($buffer, $used, $buffer.Length - $used)
        $read = $Socket.ReceiveAsync($segment, $Connection.Token)
        if ($Heartbeat.interval) {
            # Keep exactly one receive pending while sending application heartbeats.
            do {
                $wait = [math]::Max(0, $Heartbeat.interval - $Heartbeat.clock.ElapsedMilliseconds)
                if ($wait -eq 0) {
                    Send-ControlMessage $Socket @{type = 'ping'} $Connection.Token
                    $Heartbeat.clock.Restart()
                    $wait = $Heartbeat.interval
                }
            } until ($read.Wait([int]$wait))
        }
        $frame = $read.GetAwaiter().GetResult()
        if ($frame.MessageType -eq 'Close') { throw 'Control connection closed' }
        if ($frame.MessageType -ne 'Text') { throw 'Expected a text control message' }
        $used += $frame.Count
        if (!$frame.EndOfMessage -and $used -eq $buffer.Length) { throw 'Control event exceeds size limit' }
    } until ($frame.EndOfMessage)
    return ([Text.Encoding]::UTF8.GetString($buffer, 0, $used) | ConvertFrom-Json)
}

function Wait-ScanJob($JobID, $BaseURL, $Session, $Headers) {
    $endpoint = [UriBuilder]::new("$BaseURL/api/v1/control")
    if ($endpoint.Scheme -eq 'https') { $endpoint.Scheme = 'wss' } else { $endpoint.Scheme = 'ws' }
    $lifetime = [Threading.CancellationTokenSource]::new([timespan]::FromHours(1))
    $backoff = 500
    try {
        while (!$lifetime.IsCancellationRequested) {
            $socket = [Net.WebSockets.ClientWebSocket]::new()
            $connection = [Threading.CancellationTokenSource]::CreateLinkedTokenSource($lifetime.Token)
            try {
                $socket.Options.Cookies = $Session.Cookies
                $socket.Options.SetRequestHeader('Origin', $BaseURL)
                $socket.Options.AddSubProtocol('musicfs.control.v1')
                if ($socket.Options.PSObject.Properties['CollectHttpResponseDetails']) {
                    $socket.Options.CollectHttpResponseDetails = $true
                }
                $connection.CancelAfter(10000)
                [void]$socket.ConnectAsync($endpoint.Uri, $connection.Token).GetAwaiter().GetResult()
                Send-ControlMessage $socket @{type = 'auth'; csrfToken = $Headers['X-CSRF-Token']} $connection.Token
                $heartbeat = @{interval = 0; clock = [Diagnostics.Stopwatch]::StartNew()}
                $ready = Read-ControlMessage $socket $connection $heartbeat
                if ($ready.type -ne 'ready' -or $ready.heartbeatIntervalMs -le 0 -or $ready.heartbeatTimeoutMs -le $ready.heartbeatIntervalMs) {
                    throw 'Invalid control handshake'
                }
                $heartbeat.interval = [int]$ready.heartbeatIntervalMs
                $heartbeat.clock.Restart()
                $backoff = 500
                # Reconcile the accepted job after every handshake, including reconnects.
                $refresh = $true
                while (!$connection.IsCancellationRequested) {
                    $connection.CancelAfter([int]$ready.heartbeatTimeoutMs)
                    if ($refresh) {
                        $job = Invoke-RestMethod "$BaseURL/api/v1/scan-jobs/$JobID" -Headers $Headers -WebSession $Session -TimeoutSec 15
                        if ($job.status -notin @('queued', 'running', 'retry_wait')) { return $job }
                    }
                    $message = Read-ControlMessage $socket $connection $heartbeat
                    $refresh = $message.type -eq 'event' -and $message.event -eq 'activity.changed' -and 'scans' -in $message.data.resources
                }
            } catch {
                $httpStatus = if ($_.Exception.Response) { [int]$_.Exception.Response.StatusCode } else { [int]$socket.HttpStatusCode }
                if ($socket.CloseStatus -eq 'PolicyViolation' -or $httpStatus -in 400, 401, 403, 404) { throw }
                Write-Verbose "Scan control disconnected for job $JobID. Reconnecting."
            } finally {
                $socket.Dispose()
                $connection.Dispose()
            }
            [void]$lifetime.Token.WaitHandle.WaitOne($backoff)
            $backoff = [math]::Min($backoff * 2, 8000)
        }
        throw "Scan wait timed out for job $JobID; query the job before retrying"
    } finally { $lifetime.Dispose() }
}

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
    if ($PSBoundParameters.ContainsKey('ExcludePatterns')) { $options.excludePatterns = @($ExcludePatterns) }
    $body = @{
        mode = $Mode
        schemaRevision = $selected.configuration.schemaRevision
        rootIds = @($RootIDs)
        options = $options
    } | ConvertTo-Json -Depth 10 -Compress
    $headers['Idempotency-Key'] = [guid]::NewGuid().ToString()
    $provider = [uri]::EscapeDataString($ProviderID)
    $accepted = Invoke-RestMethod "$BaseURL/api/v1/provider-instances/$provider/scan-jobs" -Method Post -ContentType 'application/json' -Body $body -Headers $headers -WebSession $session -TimeoutSec 15
    $job = Wait-ScanJob $accepted.jobId $BaseURL $session $headers
    if ($job.status -ne 'succeeded') { throw "Scan ended with status $($job.status)" }
    $job | ConvertTo-Json -Depth 5
} finally {
    Invoke-RestMethod "$BaseURL/api/v1/auth/session" -Method Delete -Headers $headers -WebSession $session -TimeoutSec 15 | Out-Null
}
