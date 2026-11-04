# common.ps1 — Helpers shared by the bridge/scripts scripts.
# Dot-sourced after auth.ps1.

# ConvertTo-BridgeSplat builds the cmdlet parameters from the script parameters
# that the Go client actually sent. Attributes absent from the Terraform
# configuration are not sent, so Teams keeps its current value for them.
function ConvertTo-BridgeSplat {
    param(
        $Bound,
        [string[]]$Bool   = @(),
        [string[]]$Int    = @(),
        [string[]]$String = @()
    )
    $splat = @{}
    foreach ($k in $Bool)   { if ($Bound.ContainsKey($k)) { $splat[$k] = ([string]$Bound[$k] -eq "true") } }
    foreach ($k in $Int)    { if ($Bound.ContainsKey($k)) { $splat[$k] = [uint32]$Bound[$k] } }
    foreach ($k in $String) { if ($Bound.ContainsKey($k)) { $splat[$k] = [string]$Bound[$k] } }
    $splat
}

# ConvertFrom-BridgeJson decodes a base64 JSON value sent by the Go client.
function ConvertFrom-BridgeJson {
    param([string]$Value)
    if (-not $Value) { return $null }
    $json = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($Value))
    return ($json | ConvertFrom-Json)
}

# ConvertFrom-BridgeValues turns the base64 JSON object of parameter values sent
# by the generic resources into a splat hashtable. JSON arrays become string
# arrays; an empty array clears a multi-valued parameter.
function ConvertFrom-BridgeValues {
    param([string]$Value)
    $splat = @{}
    $object = ConvertFrom-BridgeJson $Value
    if ($null -eq $object) { return $splat }
    foreach ($p in $object.PSObject.Properties) {
        $v = $p.Value
        if ($v -is [array]) { $v = if ($v.Count -gt 0) { [string[]]$v } else { $null } }
        $splat[$p.Name] = $v
    }
    $splat
}

# ConvertTo-BridgeValue normalizes a property value for JSON: booleans and
# numbers stay typed, collections become string arrays, anything else (enums,
# TimeSpan, ...) becomes a string.
function ConvertTo-BridgeValue {
    param($Value)
    if ($null -eq $Value) { return $null }
    if ($Value -is [bool] -or $Value -is [string]) { return $Value }
    if ($Value -is [int] -or $Value -is [long] -or $Value -is [uint32] -or $Value -is [uint64] -or
        $Value -is [int16] -or $Value -is [uint16] -or $Value -is [byte]) { return [long]$Value }
    if ($Value -is [System.Collections.IEnumerable]) {
        return ,([string[]]@($Value | Where-Object { $null -ne $_ } | ForEach-Object { $_.ToString() }))
    }
    return $Value.ToString()
}

# Test-BridgeNotFound reports whether an error means the object does not exist.
function Test-BridgeNotFound {
    param([string]$Message)
    $Message -match "not found|couldn't be found|could not be found|does not exist|doesn't exist|cannot find"
}

# Get-BridgeProp reads a property that may be missing on older MicrosoftTeams
# module versions; under StrictMode a direct access to it would throw.
function Get-BridgeProp {
    param($Object, [string]$Name)
    $p = $Object.PSObject.Properties[$Name]
    if ($null -ne $p) { $p.Value } else { $null }
}

# Invoke-BridgeWrite runs a Teams write cmdlet and retries it when Teams reports
# a concurrency conflict (409 "Item has changed" / "create conflict"). Teams
# keeps all policies of one type in a single tenant document, so a change made
# at the same moment by an administrator or another tool makes the write fail.
# The provider already serializes its own writes; this covers everything else.
function Invoke-BridgeWrite {
    param([scriptblock]$Write, [int]$MaxAttempts = 5)
    for ($attempt = 1; ; $attempt++) {
        try {
            return & $Write
        } catch {
            $conflict = $_.Exception.Message -match '409|Conflict|Item has changed'
            if (-not $conflict -or $attempt -ge $MaxAttempts) { throw }
            Start-Sleep -Milliseconds (Get-Random -Minimum (500 * $attempt) -Maximum (1500 * $attempt))
        }
    }
}

# Invoke-BridgeBlockObject runs the actions of cs_object.ps1 for a Teams object
# with block lists: lists of objects that Teams builds in memory, such as the
# normalization rules of a dial plan. $Blocks maps each block parameter to
# Build, a script block that makes one object from a hashtable of its
# parameters, and Fields, the properties read back. A block list is written as
# an array, which replaces the whole list in its order; an empty one clears it.
function Invoke-BridgeBlockObject {
    param([string]$Action, [string]$Noun, [string]$Identity, [string]$Values, [string]$Properties, [hashtable]$Blocks)

    $splat = @{}
    $raw = ConvertFrom-BridgeJson $Values
    if ($null -ne $raw) {
        foreach ($p in $raw.PSObject.Properties) {
            $v = $p.Value
            if ($Blocks.ContainsKey($p.Name)) {
                $build = $Blocks[$p.Name].Build
                $v = @(foreach ($item in @($v)) {
                    if ($null -eq $item) { continue }
                    $params = @{}
                    foreach ($f in $item.PSObject.Properties) { $params[$f.Name] = $f.Value }
                    & $build $params
                })
            } elseif ($v -is [array]) {
                $v = if ($v.Count -gt 0) { [string[]]$v } else { $null }
            }
            $splat[$p.Name] = $v
        }
    }
    $props = @(ConvertFrom-BridgeJson $Properties)

    $read = {
        $object = & "Get-$Noun" -Identity $Identity -ErrorAction Stop
        $out = [ordered]@{ Name = ([string](Get-BridgeProp $object "Identity")) -replace '^Tag:', '' }
        foreach ($p in $props) {
            $value = Get-BridgeProp $object $p
            if ($Blocks.ContainsKey($p)) {
                $out[$p] = @(foreach ($item in @($value)) {
                    if ($null -eq $item) { continue }
                    $fields = [ordered]@{}
                    foreach ($f in $Blocks[$p].Fields) { $fields[$f] = ConvertTo-BridgeValue (Get-BridgeProp $item $f) }
                    [pscustomobject]$fields
                })
            } else {
                $out[$p] = ConvertTo-BridgeValue $value
            }
        }
        ConvertTo-Json -Compress -Depth 5 -InputObject $out
    }

    switch ($Action) {
        "Get" {
            try { & $read } catch {
                if (Test-BridgeNotFound $_.Exception.Message) { return }
                throw
            }
        }
        "Create" {
            if ($Identity -eq "Global") {
                if ($splat.Count -gt 0) { Invoke-BridgeWrite { & "Set-$Noun" -Identity $Identity @splat } | Out-Null }
            } else {
                Invoke-BridgeWrite { & "New-$Noun" -Identity $Identity @splat } | Out-Null
            }
            & $read
        }
        "Update" {
            if ($splat.Count -gt 0) { Invoke-BridgeWrite { & "Set-$Noun" -Identity $Identity @splat } | Out-Null }
            & $read
        }
        "Delete" {
            if ($Identity -eq "Global") { throw "The Global $Noun cannot be deleted." }
            try {
                Invoke-BridgeWrite { & "Remove-$Noun" -Identity $Identity } | Out-Null
            } catch {
                if (-not (Test-BridgeNotFound $_.Exception.Message)) { throw }
            }
            "{}"
        }
    }
}
