# bridge_host.ps1 — Persistent host for the PowerShell bridge scripts.
#
# The Go client starts a few long-lived PowerShell processes running this script
# and sends them requests, instead of starting one process per Terraform
# operation. The Teams connection (auth.ps1) is then opened once per process
# and reused, which removes the module load and Connect-MicrosoftTeams cost from
# every operation after the first.
#
# PROTOCOL (one request at a time, everything ASCII):
#   stdin  : one line per request = base64(UTF-8 JSON)
#            {"id": 1, "script": "messaging_policy.ps1", "params": {"Action": "Get", ...}}
#   stdout : one line per response = "<nonce> " + base64(UTF-8 JSON)
#            {"id": 1, "exitCode": 0, "stdout": "...", "stderr": "..."}
#   On startup the host sends {"id": 0, "ready": true}.
#   Lines on stdout without the nonce prefix are noise (e.g. text written straight
#   to the console by a module) and are ignored by the client.
#   EOF on stdin = shutdown: the host disconnects from Teams and exits.
#
# The nonce comes from TEAMS_BRIDGE_NONCE, a random value chosen by the client
# for each process, so script output can never be mistaken for a response.
# Secrets are never in requests: auth.ps1 reads them from the environment.

param(
    [string]$TenantId              = "",
    [string]$ClientId              = "",
    [string]$CertificatePath       = "",
    [string]$CertificateThumbprint = "",
    [string]$Environment           = "global"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference    = "SilentlyContinue"

$nonce = $env:TEAMS_BRIDGE_NONCE
if (-not $nonce) {
    [Console]::Error.WriteLine("TEAMS_BRIDGE_NONCE is not set; bridge_host.ps1 must be started by the provider")
    exit 2
}

$utf8     = New-Object System.Text.UTF8Encoding($false)
$protoOut = [Console]::Out
$origErr  = [Console]::Error
$stdin    = [Console]::In

# Non-secret connection parameters, added to every script invocation.
$authParams = @{
    TenantId              = $TenantId
    ClientId              = $ClientId
    CertificatePath       = $CertificatePath
    CertificateThumbprint = $CertificateThumbprint
    Environment           = $Environment
}

function Send-Response($Response) {
    $json = $Response | ConvertTo-Json -Compress -Depth 4
    $protoOut.WriteLine($nonce + " " + [Convert]::ToBase64String($utf8.GetBytes($json)))
    $protoOut.Flush()
}

Send-Response @{ id = 0; ready = $true }

while ($true) {
    $line = $stdin.ReadLine()
    if ($null -eq $line) { break }          # client closed stdin: shut down
    if (-not $line.Trim()) { continue }

    $id      = -1
    $code    = 0
    $outText = ""
    $errBuf  = New-Object System.IO.StringWriter
    $noise   = New-Object System.IO.StringWriter

    try {
        # System.Text.Json instead of ConvertFrom-Json: ConvertFrom-Json turns
        # ISO 8601 strings into DateTime values, and casting them back to a
        # string drops the time zone (an expiration date would move by the
        # local UTC offset). Parameters must reach the script unchanged.
        $req  = [System.Text.Json.JsonDocument]::Parse($utf8.GetString([Convert]::FromBase64String($line.Trim()))).RootElement
        $id   = $req.GetProperty("id").GetInt32()
        $name = $req.GetProperty("script").GetString()

        # Only bare script names from this folder; never this host itself.
        if ($name -notmatch '^[a-z0-9_]+\.ps1$' -or $name -eq "bridge_host.ps1") {
            throw "invalid script name '$name'"
        }
        $path = Join-Path $PSScriptRoot $name
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "unknown bridge script '$name'" }

        $splat = @{} + $authParams
        $params = [System.Text.Json.JsonElement]::new()
        if ($req.TryGetProperty("params", [ref]$params) -and $params.ValueKind -eq [System.Text.Json.JsonValueKind]::Object) {
            foreach ($p in $params.EnumerateObject()) {
                $splat[$p.Name] = if ($p.Value.ValueKind -eq [System.Text.Json.JsonValueKind]::String) { $p.Value.GetString() } else { $p.Value.GetRawText() }
            }
        }

        # Capture what the script writes straight to the console streams, so
        # each response carries only its own messages.
        [Console]::SetError($errBuf)
        [Console]::SetOut($noise)
        $global:LASTEXITCODE = 0

        # The script runs in its own scope: its variables, StrictMode and
        # preferences do not leak into the next request. 'exit N' inside it
        # only ends the script and sets $LASTEXITCODE.
        $result = & $path @splat 2>&1 3>$null 4>$null 5>$null 6>$null
        $code = if ($null -eq $LASTEXITCODE) { 0 } else { [int]$LASTEXITCODE }

        $lines = foreach ($r in $result) {
            if ($r -is [System.Management.Automation.ErrorRecord]) { $errBuf.WriteLine($r.ToString()) }
            else { "$r" }
        }
        $outText = @($lines) -join "`n"
    } catch {
        $code = 1
        $errBuf.WriteLine($_.Exception.Message)
    } finally {
        [Console]::SetError($origErr)
        [Console]::SetOut($protoOut)
    }

    # Console.Out noise would have corrupted stdout in one-shot mode; keep it
    # only as diagnostic context on failures.
    $stderrText = $errBuf.ToString()
    if ($code -ne 0 -and $noise.ToString().Trim()) { $stderrText += "`n" + $noise.ToString() }

    Send-Response @{ id = $id; exitCode = $code; stdout = $outText; stderr = $stderrText.Trim() }
}

# Graceful shutdown: release the Teams session instead of letting it expire.
try {
    if (Get-Variable -Name TeamsBridgeConnected -Scope Global -ErrorAction SilentlyContinue) {
        Disconnect-MicrosoftTeams -ErrorAction SilentlyContinue | Out-Null
    }
} catch { }
exit 0
