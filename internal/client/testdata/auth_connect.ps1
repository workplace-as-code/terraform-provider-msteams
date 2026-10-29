# auth_connect.ps1 - runs auth.ps1 against a fake Connect-MicrosoftTeams and
# prints, as JSON, the parameters it received (run by auth_connect_test.go).
#   -Case token             : TEAMS_BRIDGE_GRAPH_TOKEN and TEAMS_BRIDGE_TEAMS_TOKEN are set
#   -Case token-noclientid  : the same, without a client ID (system-assigned managed identity)
#   -Case base64            : TEAMS_BRIDGE_CERTIFICATE_BASE64 holds a password-protected .pfx
#   -Case base64-wrapped    : the same, cut in lines of 76 characters like `base64` does
#   -Case base64-nopassword : a .pfx exported without a password
param([string]$Script, [string]$Case)

$ErrorActionPreference = "Stop"

function Connect-MicrosoftTeams {
    param($TenantId, $ApplicationId, $Certificate, $CertificateThumbprint, $TeamsEnvironmentName, [string[]]$AccessTokens)
    $global:Connected = [ordered]@{
        TenantId      = $TenantId
        ApplicationId = $ApplicationId
        AccessTokens  = $AccessTokens
        Thumbprint    = if ($Certificate) { $Certificate.Thumbprint } else { $null }
    }
}

$expected = $null
switch ($Case) {
    { $_ -like "token*" } {
        $env:TEAMS_BRIDGE_GRAPH_TOKEN = "graph-1"
        $env:TEAMS_BRIDGE_TEAMS_TOKEN = "teams-1"
    }
    { $_ -like "base64*" } {
        $rsa  = [System.Security.Cryptography.RSA]::Create(2048)
        $req  = [System.Security.Cryptography.X509Certificates.CertificateRequest]::new(
            "CN=auth-connect-test", $rsa,
            [System.Security.Cryptography.HashAlgorithmName]::SHA256,
            [System.Security.Cryptography.RSASignaturePadding]::Pkcs1)
        $cert = $req.CreateSelfSigned([DateTimeOffset]::Now, [DateTimeOffset]::Now.AddDays(1))
        $password = if ($Case -eq "base64-nopassword") { $null } else { "pw" }
        $pfx  = $cert.Export([System.Security.Cryptography.X509Certificates.X509ContentType]::Pfx, $password)
        $b64  = [Convert]::ToBase64String($pfx)
        if ($Case -eq "base64-wrapped") {
            $b64 = (($b64 -split '(.{76})') | Where-Object { $_ }) -join "`n"
        }
        $env:TEAMS_BRIDGE_CERTIFICATE_BASE64   = $b64
        $env:TEAMS_BRIDGE_CERTIFICATE_PASSWORD = $password
        $expected = $cert.Thumbprint
    }
    default { throw "unknown case $Case" }
}

$clientId = if ($Case -eq "token-noclientid") { "" } else { "00000000-0000-0000-0000-000000000001" }
. $Script -TenantId contoso.onmicrosoft.com -ClientId $clientId

ConvertTo-Json -Compress -InputObject @{ connected = $global:Connected; expectedThumbprint = $expected }
