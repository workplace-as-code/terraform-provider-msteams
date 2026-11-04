# emergency_call_routing_policy.ps1 - CsTeamsEmergencyCallRoutingPolicy, with the
# same contract as cs_object.ps1. Emergency numbers are built in memory
# (New-CsTeamsEmergencyNumber) and written as a list.

param(
    [string]$TenantId              = "",
    [string]$ClientId              = "",
    [string]$CertificateThumbprint = "",
    [string]$CertificatePath       = "",
    [string]$CertificatePassword   = "",
    [string]$Environment           = "global",

    [Parameter(Mandatory)] [ValidateSet("Get","Create","Update","Delete")] [string]$Action,

    [string]$Noun       = "CsTeamsEmergencyCallRoutingPolicy",
    [string]$Identity   = "Global",
    [string]$Values     = "",
    [string]$Properties = ""
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. "$PSScriptRoot/auth.ps1" -TenantId $TenantId -ClientId $ClientId -CertificatePath $CertificatePath -CertificatePassword $CertificatePassword -CertificateThumbprint $CertificateThumbprint -Environment $Environment
. "$PSScriptRoot/common.ps1"

$blocks = @{
    EmergencyNumbers = @{
        Build  = { param($p) New-CsTeamsEmergencyNumber @p }
        Fields = "EmergencyDialString", "EmergencyDialMask", "OnlinePSTNUsage"
    }
}
Invoke-BridgeBlockObject -Action $Action -Noun $Noun -Identity $Identity -Values $Values -Properties $Properties -Blocks $blocks
