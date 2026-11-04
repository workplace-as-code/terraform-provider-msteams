# online_pstn_usage.ps1 - One PSTN usage of the tenant: a name in the global
# list of Get-CsOnlinePstnUsage, which voice routes and voice routing
# policies refer to.
#
#   -Name  the usage; Get returns {"Name": "<usage as Teams stores it>"},
#          or nothing when the tenant does not have it

param(
    [string]$TenantId              = "",
    [string]$ClientId              = "",
    [string]$CertificateThumbprint = "",
    [string]$CertificatePath       = "",
    [string]$CertificatePassword   = "",
    [string]$Environment           = "global",

    [Parameter(Mandatory)] [ValidateSet("Get","Create","Delete")] [string]$Action,
    [Parameter(Mandatory)] [string]$Name
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. "$PSScriptRoot/auth.ps1" -TenantId $TenantId -ClientId $ClientId -CertificatePath $CertificatePath -CertificatePassword $CertificatePassword -CertificateThumbprint $CertificateThumbprint -Environment $Environment
. "$PSScriptRoot/common.ps1"

function Find-BridgeUsage {
    @(Get-BridgeProp (Get-CsOnlinePstnUsage -Identity Global) "Usage" @()) | Where-Object { "$_" -eq $Name } | Select-Object -First 1
}

switch ($Action) {
    "Get" {
        $usage = Find-BridgeUsage
        if ($null -ne $usage) { ConvertTo-Json -Compress -InputObject @{ Name = "$usage" } }
    }
    "Create" {
        if ($null -eq (Find-BridgeUsage)) {
            Invoke-BridgeWrite { Set-CsOnlinePstnUsage -Identity Global -Usage @{ Add = $Name } } | Out-Null
        }
        $usage = Find-BridgeUsage
        if ($null -eq $usage) { throw "The PSTN usage '$Name' was added but cannot be read back." }
        ConvertTo-Json -Compress -InputObject @{ Name = "$usage" }
    }
    "Delete" {
        $usage = Find-BridgeUsage
        if ($null -ne $usage) {
            Invoke-BridgeWrite { Set-CsOnlinePstnUsage -Identity Global -Usage @{ Remove = "$usage" } } | Out-Null
        }
        "{}"
    }
}
