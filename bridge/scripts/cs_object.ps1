# cs_object.ps1 — Generic CRUD for Teams policies and configurations
# (CsTeamsMeetingPolicy, CsTeamsClientConfiguration, ...).
#
# The Go resource describes the object with a cmdlet noun and the list of
# properties it manages:
#   -Noun        cmdlet noun, e.g. "CsTeamsMeetingPolicy"
#   -Identity    policy name, or "Global" for configurations and the org-wide policy
#   -Values      base64 JSON object: cmdlet parameter -> value, only the
#                attributes set in the Terraform configuration
#   -Properties  base64 JSON array: properties returned to Terraform
#
# "Global" always exists: Create sets it instead of creating it, and Delete is
# never called for it (the Go resource only removes it from state).

param(
    [string]$TenantId              = "",
    [string]$ClientId              = "",
    [string]$CertificateThumbprint = "",
    [string]$CertificatePath       = "",
    [string]$CertificatePassword   = "",
    [string]$Environment           = "global",

    [Parameter(Mandatory)] [ValidateSet("Get","Create","Update","Delete")] [string]$Action,

    [Parameter(Mandatory)] [ValidatePattern('^Cs[A-Za-z]+$')] [string]$Noun,
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
$isGlobal = $Identity -eq "Global"

function Read-BridgeObject {
    $object = & "Get-$Noun" -Identity $Identity -ErrorAction Stop
    $out = [ordered]@{
        # Teams returns custom policies as "Tag:<name>"; Terraform uses the bare name.
        Name = ([string](Get-BridgeProp $object "Identity")) -replace '^Tag:', ''
    }
    foreach ($p in $props) { $out[$p] = ConvertTo-BridgeValue (Get-BridgeProp $object $p) }
    ConvertTo-Json -Compress -Depth 4 -InputObject $out
}

switch ($Action) {
    "Get" {
        try {
            Read-BridgeObject
        } catch {
            if (Test-BridgeNotFound $_.Exception.Message) { exit 0 }
            throw
        }
    }

    "Create" {
        if ($isGlobal) {
            if ($splat.Count -gt 0) { Invoke-BridgeWrite { & "Set-$Noun" -Identity $Identity @splat } | Out-Null }
        } else {
            Invoke-BridgeWrite { & "New-$Noun" -Identity $Identity @splat } | Out-Null
        }
        Read-BridgeObject
    }

    "Update" {
        if ($splat.Count -gt 0) {
            Invoke-BridgeWrite { & "Set-$Noun" -Identity $Identity @splat } | Out-Null
        }
        Read-BridgeObject
    }

    "Delete" {
        if ($isGlobal) { throw "The Global $Noun cannot be deleted." }
        $remove = @{ Identity = $Identity }
        if ((Get-Command "Remove-$Noun").Parameters.ContainsKey("Force")) { $remove["Force"] = $true }
        try {
            Invoke-BridgeWrite { & "Remove-$Noun" @remove } | Out-Null
        } catch {
            if (-not (Test-BridgeNotFound $_.Exception.Message)) { throw }
        }
        "{}"
    }
}
