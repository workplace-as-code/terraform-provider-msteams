# auth.ps1 — Teams authentication via the MicrosoftTeams PowerShell module
# Dot-sourced by all bridge/scripts scripts.
#
# RUNTIME REQUIREMENTS:
#   pwsh (PowerShell 7+) and the MicrosoftTeams module (Install-Module MicrosoftTeams)
#
# App-only authentication, in this order:
#   - two access tokens (Microsoft Graph, Teams) obtained by the provider with
#     OIDC or a managed identity (TEAMS_BRIDGE_GRAPH_TOKEN, TEAMS_BRIDGE_TEAMS_TOKEN);
#   - a certificate: Windows store thumbprint, base64 content of a .pfx
#     (TEAMS_BRIDGE_CERTIFICATE_BASE64), or .pfx file.
# The identity needs the Microsoft Graph application permission
# Organization.Read.All (admin consent) and the Teams Administrator Entra role.
# That is enough for the *-Cs* cmdlets used by this provider.

param(
    [string]$TenantId              = "",
    [string]$ClientId              = "",
    [string]$CertificatePath       = "",
    [string]$CertificatePassword   = "",
    [string]$CertificateThumbprint = "",
    [string]$Environment           = "global"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# The connection runs in a child scope. This file is dot-sourced into every
# bridge script: a variable assigned here would overwrite the script's own
# parameter of the same name, without regard to case (msexchangeserver once
# created a transport rule named after the Exchange snap-in this way).
& {
    param([string]$TenantId, [string]$ClientId, [string]$CertificatePath, [string]$CertificatePassword, [string]$CertificateThumbprint, [string]$Environment)

    # Secrets are passed by the Go client through environment variables, never on the
    # command line (command lines are visible to every local user and are logged by
    # process-creation auditing). Explicit parameters still win, for manual testing.
    if (-not $CertificatePassword -and $env:TEAMS_BRIDGE_CERTIFICATE_PASSWORD) { $CertificatePassword = $env:TEAMS_BRIDGE_CERTIFICATE_PASSWORD }
    $graphToken        = $env:TEAMS_BRIDGE_GRAPH_TOKEN
    $teamsToken        = $env:TEAMS_BRIDGE_TEAMS_TOKEN
    $certificateBase64 = $env:TEAMS_BRIDGE_CERTIFICATE_BASE64

    # Commercial and GCC tenants use the default endpoints.
    $teamsEnvironments = @{
        "global"  = ""
        "gcc"     = ""
        "gcchigh" = "TeamsGCCH"
        "dod"     = "TeamsDOD"
    }
    if (-not $teamsEnvironments.ContainsKey($Environment)) {
        throw "Invalid environment: '$Environment'. Valid values: global, gcc, gcchigh, dod"
    }

    # Skip if this process is already connected. With the persistent bridge host,
    # one PowerShell process serves many requests and reuses the first connection.
    if (Get-Variable -Name TeamsBridgeConnected -Scope Global -ErrorAction SilentlyContinue) { return }

    if (-not $TenantId) { throw "tenant_id is required" }

    if ($graphToken -and $teamsToken) {
        # OIDC or managed identity: the provider obtained these access tokens
        # and starts a new process before the first one expires. A token
        # already names its identity: a system-assigned managed identity has
        # no client ID at all. The tokens also name the tenant: with -TenantId,
        # Connect-MicrosoftTeams fails with "Not supported tenant type".
        $connectParams = @{ AccessTokens = @($graphToken, $teamsToken) }
    } else {
        if (-not $ClientId) { throw "client_id is required" }
        $connectParams = @{
            TenantId      = $TenantId
            ApplicationId = $ClientId
        }
        if ($teamsEnvironments[$Environment]) { $connectParams["TeamsEnvironmentName"] = $teamsEnvironments[$Environment] }

        # EphemeralKeySet keeps the private key out of the key store, but macOS does
        # not support it: there the default loads the key into a temporary keychain.
        $certFlags = if ($IsMacOS) {
            [System.Security.Cryptography.X509Certificates.X509KeyStorageFlags]::DefaultKeySet
        } else {
            [System.Security.Cryptography.X509Certificates.X509KeyStorageFlags]::EphemeralKeySet
        }
        if ($CertificateThumbprint) {
            # Windows certificate store — thumbprint lookup
            $connectParams["CertificateThumbprint"] = $CertificateThumbprint
        } elseif ($certificateBase64) {
            # Content of the .pfx, decoded in memory: no file on disk. A value cut
            # in lines (base64 without -A, a vault) is accepted.
            $bytes = [Convert]::FromBase64String(($certificateBase64 -replace '\s', ''))
            $connectParams["Certificate"] = [System.Security.Cryptography.X509Certificates.X509Certificate2]::new(
                [byte[]]$bytes,
                $CertificatePassword,
                $certFlags
            )
        } elseif ($CertificatePath) {
            $connectParams["Certificate"] = [System.Security.Cryptography.X509Certificates.X509Certificate2]::new(
                $CertificatePath,
                $CertificatePassword,
                $certFlags
            )
        } else {
            throw "Either certificate_path, certificate_base64 or certificate_thumbprint is required"
        }
    }

    try {
        Connect-MicrosoftTeams @connectParams | Out-Null
    } catch {
        throw "Failed to connect to Microsoft Teams: $_"
    }
    $global:TeamsBridgeConnected = $true
} $TenantId $ClientId $CertificatePath $CertificatePassword $CertificateThumbprint $Environment
