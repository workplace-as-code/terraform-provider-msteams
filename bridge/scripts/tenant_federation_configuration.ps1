# tenant_federation_configuration.ps1 — Read/write CsTenantFederationConfiguration
# (Teams external access). Same contract as cs_object.ps1, with the domain lists
# translated to and from their Teams object types:
#   - AllowedDomains: "AllowAllKnownDomains" when the list is empty, otherwise
#     an allow list (set with -AllowedDomainsAsAList)
#   - BlockedDomains: domain pattern objects, returned as plain domain names

param(
    [string]$TenantId              = "",
    [string]$ClientId              = "",
    [string]$CertificateThumbprint = "",
    [string]$CertificatePath       = "",
    [string]$CertificatePassword   = "",
    [string]$Environment           = "global",

    [Parameter(Mandatory)] [ValidateSet("Get","Create","Update","Delete")] [string]$Action,

    [string]$Noun       = "CsTenantFederationConfiguration",
    [string]$Identity   = "Global",
    [string]$Values     = "",
    [string]$Properties = ""
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. "$PSScriptRoot/auth.ps1" -TenantId $TenantId -ClientId $ClientId -CertificatePath $CertificatePath -CertificatePassword $CertificatePassword -CertificateThumbprint $CertificateThumbprint -Environment $Environment
. "$PSScriptRoot/common.ps1"

$splat = ConvertFrom-BridgeValues $Values
$props = @(ConvertFrom-BridgeJson $Properties)

# ConvertTo-DomainNames returns the domain names of an allow list, a block list
# or a single domain pattern object.
function ConvertTo-DomainNames {
    param($Value)
    if ($null -eq $Value) { return @() }
    $allowed = Get-BridgeProp $Value "AllowedDomain"
    if ($null -ne $allowed) { $Value = $allowed }
    @($Value | ForEach-Object {
        $domain = Get-BridgeProp $_ "Domain"
        if ($null -ne $domain) { [string]$domain } elseif ($_ -is [string]) { $_ }
    } | Where-Object { $_ })
}

function Read-Federation {
    $cfg = Get-CsTenantFederationConfiguration -Identity Global
    $out = [ordered]@{ Name = "Global" }
    foreach ($p in $props) {
        $v = Get-BridgeProp $cfg $p
        switch ($p) {
            "AllowedDomains" {
                # AllowAllKnownDomains is reported as an empty list.
                $out[$p] = if ("$v" -match "AllowAllKnownDomains") { @() } else { @(ConvertTo-DomainNames $v) }
            }
            "BlockedDomains" { $out[$p] = @(ConvertTo-DomainNames $v) }
            default          { $out[$p] = ConvertTo-BridgeValue $v }
        }
    }
    ConvertTo-Json -Compress -Depth 4 -InputObject $out
}

switch ($Action) {
    "Get" { Read-Federation }

    { $_ -in "Create", "Update" } {
        if ($splat.ContainsKey("AllowedDomains")) {
            $allowed = @($splat["AllowedDomains"] | Where-Object { $_ })
            $splat.Remove("AllowedDomains")
            if ($allowed.Count -gt 0) {
                $splat["AllowedDomainsAsAList"] = [string[]]$allowed
            } else {
                $splat["AllowedDomains"] = New-CsEdgeAllowAllKnownDomains
            }
        }
        if ($splat.ContainsKey("BlockedDomains")) {
            $blocked = @($splat["BlockedDomains"] | Where-Object { $_ })
            $splat["BlockedDomains"] = [System.Collections.Generic.List[object]]@($blocked | ForEach-Object { New-CsEdgeDomainPattern -Domain $_ })
        }
        if ($splat.Count -gt 0) {
            Invoke-BridgeWrite { Set-CsTenantFederationConfiguration -Identity Global @splat } | Out-Null
        }
        Read-Federation
    }

    "Delete" { throw "The tenant federation configuration cannot be deleted." }
}
