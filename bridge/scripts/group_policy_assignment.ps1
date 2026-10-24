# group_policy_assignment.ps1 — CRUD for CsGroupPolicyAssignment
# A group has at most one assignment per policy type, so the assignment is
# identified by the group ID and the policy type. There is no Set cmdlet.

param(
    [string]$TenantId              = "",
    [string]$ClientId              = "",
    [string]$CertificateThumbprint = "",
    [string]$CertificatePath       = "",
    [string]$CertificatePassword   = "",
    [string]$Environment           = "global",

    [Parameter(Mandatory)] [ValidateSet("Get","Create","Delete")] [string]$Action,

    [Parameter(Mandatory)] [string]$GroupId,
    [Parameter(Mandatory)] [string]$PolicyType,
    [string]$PolicyName = "",
    [string]$Rank       = "1"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

. "$PSScriptRoot/auth.ps1" -TenantId $TenantId -ClientId $ClientId -CertificatePath $CertificatePath -CertificatePassword $CertificatePassword -CertificateThumbprint $CertificateThumbprint -Environment $Environment
. "$PSScriptRoot/common.ps1"

# Get-Assignment returns the assignment, or $null when the group has none for
# this policy type.
function Get-Assignment {
    try {
        $all = @(Get-CsGroupPolicyAssignment -GroupId $GroupId -PolicyType $PolicyType -ErrorAction Stop)
    } catch {
        if (Test-BridgeNotFound $_.Exception.Message) { return $null }
        throw
    }
    @($all | Where-Object { [string](Get-BridgeProp $_ "PolicyType") -eq $PolicyType }) | Select-Object -First 1
}

function ConvertTo-BridgeAssignment {
    param($Assignment)
    [ordered]@{
        GroupId    = [string](Get-BridgeProp $Assignment "GroupId")
        PolicyType = [string](Get-BridgeProp $Assignment "PolicyType")
        PolicyName = [string](Get-BridgeProp $Assignment "PolicyName")
        Rank       = [long](Get-BridgeProp $Assignment "Priority" (Get-BridgeProp $Assignment "Rank" 0))
    } | ConvertTo-Json -Compress
}

switch ($Action) {
    "Get" {
        $a = Get-Assignment
        if ($null -eq $a) { exit 0 }
        ConvertTo-BridgeAssignment $a
    }

    "Create" {
        Invoke-BridgeWrite {
            New-CsGroupPolicyAssignment -GroupId $GroupId -PolicyType $PolicyType -PolicyName $PolicyName -Rank ([int]$Rank)
        } | Out-Null
        # The assignment can take a few seconds to show up.
        for ($i = 0; $i -lt 10; $i++) {
            $a = Get-Assignment
            if ($null -ne $a) { break }
            Start-Sleep -Seconds 3
        }
        if ($null -eq $a) { throw "The $PolicyType assignment for group $GroupId was created but cannot be read back yet. Run terraform apply again." }
        ConvertTo-BridgeAssignment $a
    }

    "Delete" {
        try {
            Invoke-BridgeWrite { Remove-CsGroupPolicyAssignment -GroupId $GroupId -PolicyType $PolicyType } | Out-Null
        } catch {
            if (-not (Test-BridgeNotFound $_.Exception.Message)) { throw }
        }
        "{}"
    }
}
