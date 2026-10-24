# list.ps1 — Lists the import IDs of the existing objects of one type, for the
# list resources (terraform query).
#
#   -Noun  cmdlet noun, e.g. "CsTeamsMeetingPolicy": Get-<noun> returns the objects
#
# Policies are returned by name ("Global" for the org-wide policy, without the
# "Tag:" prefix of custom policies); group policy assignments as
# "<group_id>/<policy_type>". Output: JSON array of strings.

param(
    [string]$TenantId              = "",
    [string]$ClientId              = "",
    [string]$CertificateThumbprint = "",
    [string]$CertificatePath       = "",
    [string]$CertificatePassword   = "",
    [string]$Environment           = "global",

    [Parameter(Mandatory)] [ValidatePattern('^Cs[A-Za-z]+$')] [string]$Noun
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. "$PSScriptRoot/auth.ps1" -TenantId $TenantId -ClientId $ClientId -CertificatePath $CertificatePath -CertificatePassword $CertificatePassword -CertificateThumbprint $CertificateThumbprint -Environment $Environment
. "$PSScriptRoot/common.ps1"

try {
    $ids = @(
        foreach ($object in @(& "Get-$Noun")) {
            if ($Noun -eq "CsGroupPolicyAssignment") {
                "$(Get-BridgeProp $object 'GroupId' '')/$(Get-BridgeProp $object 'PolicyType' '')"
            } else {
                ([string](Get-BridgeProp $object "Identity" "")) -replace '^Tag:', ''
            }
        }
    )
    ConvertTo-Json -Compress -InputObject ([string[]]@($ids | Where-Object { $_ -and $_ -ne "/" }))
} catch {
    [Console]::Error.WriteLine("Listing $Noun failed: $($_.Exception.Message)")
    exit 1
}
