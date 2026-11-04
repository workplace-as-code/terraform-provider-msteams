# tenant_dial_plan.ps1 - CsTenantDialPlan, with the same contract as cs_object.ps1.
# Normalization rules are built in memory (New-CsVoiceNormalizationRule -InMemory)
# and written as a list, in the order Teams applies them.

param(
    [string]$TenantId              = "",
    [string]$ClientId              = "",
    [string]$CertificateThumbprint = "",
    [string]$CertificatePath       = "",
    [string]$CertificatePassword   = "",
    [string]$Environment           = "global",

    [Parameter(Mandatory)] [ValidateSet("Get","Create","Update","Delete")] [string]$Action,

    [string]$Noun       = "CsTenantDialPlan",
    [string]$Identity   = "Global",
    [string]$Values     = "",
    [string]$Properties = ""
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. "$PSScriptRoot/auth.ps1" -TenantId $TenantId -ClientId $ClientId -CertificatePath $CertificatePath -CertificatePassword $CertificatePassword -CertificateThumbprint $CertificateThumbprint -Environment $Environment
. "$PSScriptRoot/common.ps1"

$blocks = @{
    NormalizationRules = @{
        Build  = { param($p) New-CsVoiceNormalizationRule -Parent Global -InMemory @p }
        Fields = "Name", "Pattern", "Translation", "Description", "IsInternalExtension"
    }
}
Invoke-BridgeBlockObject -Action $Action -Noun $Noun -Identity $Identity -Values $Values -Properties $Properties -Blocks $blocks
