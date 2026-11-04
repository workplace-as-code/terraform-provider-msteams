package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/workplace-as-code/terraform-provider-msteams/bridge"
)

// teamsStubs replaces the MicrosoftTeams cmdlets with global functions that
// keep policies in memory and record the parameters of every call. It is run
// as a first request: the persistent host keeps the functions for the next ones.
const teamsStubs = `param([string]$TenantId, [string]$ClientId, [string]$CertificatePath,
      [string]$CertificateThumbprint, [string]$Environment, [string]$Action)
$global:calls = [System.Collections.Generic.List[object]]::new()
$global:policies = @{}
$global:clientConfig = [pscustomobject]@{ AllowDropBox = $true; AllowBox = $true; AllowGoogleDrive = $true; AllowShareFile = $true;
    AllowOrganizationTab = $true; AllowSkypeBusinessInterop = $true; AllowEmailIntoChannel = $true; RestrictedSenderList = "";
    AllowGuestUser = $true; AllowScopedPeopleSearchandAccess = $false; ContentPin = "RequiredOutsideScheduleMeeting" }
function global:Connect-MicrosoftTeams { $global:calls.Add(@{ cmd = "Connect"; params = @() }) }
function global:New-CsTeamsMessagingPolicy {
    param([string]$Identity, [bool]$AllowGiphy)
    $global:calls.Add(@{ cmd = "New"; params = @($PSBoundParameters.Keys | Sort-Object) })
    $global:policies[$Identity] = [pscustomobject]@{ Identity = "Tag:$Identity"; Description = ""; AllowGiphy = $true; GiphyRatingType = "Moderate" }
}
function global:Get-CsTeamsMessagingPolicy {
    param([string]$Identity)
    if (-not $Identity) { return @([pscustomobject]@{ Identity = "Global" }) + @($global:policies.Values) }
    if (-not $global:policies.ContainsKey($Identity)) { throw "Cannot get policy ""$Identity"" because it does not exist." }
    $global:policies[$Identity]
}
function global:Set-CsTeamsMessagingPolicy { param([string]$Identity, [bool]$AllowGiphy)
    $global:calls.Add(@{ cmd = "SetPolicy"; params = @($PSBoundParameters.Keys | Sort-Object) })
    if ($PSBoundParameters.ContainsKey("AllowGiphy")) { $global:policies[$Identity].AllowGiphy = $AllowGiphy } }
function global:Remove-CsTeamsMessagingPolicy { param([string]$Identity, [switch]$Force, [switch]$Confirm)
    if (-not $global:policies.ContainsKey($Identity)) { throw "Policy ""$Identity"" was not found." }
    $global:policies.Remove($Identity) }
function global:Get-CsTeamsClientConfiguration { param([string]$Identity) $global:clientConfig }
function global:Set-CsTeamsClientConfiguration { param([string]$Identity, [string]$ContentPin, [bool]$AllowGuestUser)
    $global:calls.Add(@{ cmd = "SetClient"; params = @($PSBoundParameters.Keys | Sort-Object) })
    if ($PSBoundParameters.ContainsKey("ContentPin")) { $global:clientConfig.ContentPin = $ContentPin } }
$global:dialPlans = @{}
$global:pstnUsages = [System.Collections.Generic.List[string]]::new()
function global:New-CsVoiceNormalizationRule {
    param([string]$Parent, [switch]$InMemory, [string]$Name, [string]$Pattern, [string]$Translation, [string]$Description, [bool]$IsInternalExtension)
    [pscustomobject]@{ Name = $Name; Pattern = $Pattern; Translation = $Translation; Description = "$Description"; IsInternalExtension = $IsInternalExtension }
}
function global:New-CsTenantDialPlan { param([string]$Identity, [string]$Description, [object[]]$NormalizationRules)
    $global:dialPlans[$Identity] = [pscustomobject]@{ Identity = "Tag:$Identity"; Description = "$Description"; NormalizationRules = @($NormalizationRules) } }
function global:Get-CsTenantDialPlan { param([string]$Identity)
    if (-not $Identity) { return @($global:dialPlans.Values) }
    if (-not $global:dialPlans.ContainsKey($Identity)) { throw """$Identity"" not found" }
    $global:dialPlans[$Identity] }
function global:Set-CsTenantDialPlan { param([string]$Identity, [string]$Description, [object[]]$NormalizationRules)
    if ($PSBoundParameters.ContainsKey("NormalizationRules")) { $global:dialPlans[$Identity].NormalizationRules = @($NormalizationRules) } }
function global:Remove-CsTenantDialPlan { param([string]$Identity)
    if (-not $global:dialPlans.ContainsKey($Identity)) { throw """$Identity"" not found" }
    $global:dialPlans.Remove($Identity) }
function global:Get-CsOnlinePstnUsage { param([string]$Identity) [pscustomobject]@{ Identity = "Global"; Usage = @($global:pstnUsages) } }
function global:Set-CsOnlinePstnUsage { param([string]$Identity, [hashtable]$Usage)
    if ($Usage.ContainsKey("Add")) { $global:pstnUsages.Add($Usage.Add) }
    if ($Usage.ContainsKey("Remove")) { [void]$global:pstnUsages.Remove($Usage.Remove) } }
'{}'
`

const callsProbe = `param([string]$TenantId, [string]$ClientId, [string]$CertificatePath,
      [string]$CertificateThumbprint, [string]$Environment, [string]$Action)
ConvertTo-Json -Compress -Depth 4 -InputObject @($global:calls)
`

func TestTeamsScriptsWithStubbedCmdlets(t *testing.T) {
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh not installed")
	}
	dir := t.TempDir()
	sub, err := fs.Sub(bridge.Scripts, bridge.ScriptsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dir, sub); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"stubs.ps1": teamsStubs, "probe_calls.ps1": callsProbe} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	c := &Client{config: Config{TenantID: "contoso", ClientID: "app", CertificateThumbprint: "ABC", Environment: "global"},
		scriptDir: dir, timeout: time.Minute, sem: make(chan struct{}, 1)}
	c.pool = newPool(c, 1, 100)
	t.Cleanup(func() {
		select {
		case w := <-c.pool.idle:
			w.kill()
		default:
		}
	})

	ctx := context.Background()
	run := func(script string, params map[string]string) string {
		t.Helper()
		out, err := c.RunScript(ctx, script, params)
		if err != nil {
			t.Fatalf("%s %v: %v", script, params, err)
		}
		return string(out)
	}
	type call struct {
		Cmd    string   `json:"cmd"`
		Params []string `json:"params"`
	}
	lastCall := func() call {
		t.Helper()
		var calls []call
		if err := json.Unmarshal([]byte(run("probe_calls.ps1", map[string]string{"Action": "Get"})), &calls); err != nil {
			t.Fatal(err)
		}
		return calls[len(calls)-1]
	}

	run("stubs.ps1", map[string]string{"Action": "Get"})

	// obj builds the parameters of the generic cs_object.ps1 script.
	obj := func(action, noun, identity string, values map[string]any, props ...string) map[string]string {
		p := map[string]string{"Action": action, "Noun": noun, "Identity": identity, "Properties": b64JSON(t, props)}
		if values != nil {
			p["Values"] = b64JSON(t, values)
		}
		return p
	}
	const policy = "CsTeamsMessagingPolicy"

	// Create sends only the attributes present in the request.
	out := run("cs_object.ps1", obj("Create", policy, "Students", map[string]any{"AllowGiphy": false}, "AllowGiphy", "GiphyRatingType"))
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("create output %q: %v", out, err)
	}
	if got["Name"] != "Students" {
		t.Errorf("Name = %v, want the bare name without Tag:", got["Name"])
	}
	if got["AllowGiphy"] != true || got["GiphyRatingType"] != "Moderate" {
		t.Errorf("properties = %v", got)
	}
	if c := lastCall(); c.Cmd != "New" || len(c.Params) != 2 || c.Params[0] != "AllowGiphy" || c.Params[1] != "Identity" {
		t.Errorf("New-CsTeamsMessagingPolicy called with %v, want only AllowGiphy and Identity", c)
	}

	// list.ps1 returns the bare names, the org-wide policy included.
	var ids []string
	if err := json.Unmarshal([]byte(run("list.ps1", map[string]string{"Noun": policy})), &ids); err != nil || strings.Join(ids, ",") != "Global,Students" {
		t.Errorf("list.ps1 returned %v (%v), want [Global Students]", ids, err)
	}

	// Update with no attribute does not call Set at all.
	run("cs_object.ps1", obj("Update", policy, "Students", map[string]any{}, "AllowGiphy"))
	if c := lastCall(); c.Cmd == "SetPolicy" {
		t.Errorf("Set-CsTeamsMessagingPolicy called without attributes: %v", c)
	}

	// Update sends the boolean as a real boolean.
	out = run("cs_object.ps1", obj("Update", policy, "Students", map[string]any{"AllowGiphy": false}, "AllowGiphy"))
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["AllowGiphy"] != false {
		t.Errorf("update output %q: %v", out, err)
	}

	// Get on a missing policy honours the not-found contract: empty stdout, no error.
	if out := run("cs_object.ps1", obj("Get", policy, "Missing", nil)); !IsNotFound([]byte(out)) {
		t.Errorf("Get on a missing policy returned %q, want empty output", out)
	}

	// Delete is idempotent.
	run("cs_object.ps1", obj("Delete", policy, "Students", nil))
	run("cs_object.ps1", obj("Delete", policy, "Students", nil))

	// A configuration only receives the configured attribute.
	out = run("cs_object.ps1", obj("Update", "CsTeamsClientConfiguration", "Global", map[string]any{"ContentPin": "AlwaysRequired"}, "ContentPin", "AllowGuestUser"))
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("client configuration output %q: %v", out, err)
	}
	if got["ContentPin"] != "AlwaysRequired" || got["AllowGuestUser"] != true {
		t.Errorf("configuration = %v", got)
	}
	if c := lastCall(); c.Cmd != "SetClient" || len(c.Params) != 2 {
		t.Errorf("Set-CsTeamsClientConfiguration called with %v, want only ContentPin and Identity", c)
	}

	// A dial plan writes its normalization rules as objects built in memory, in
	// order, and reads them back as a list, even with one rule.
	type rule struct{ Name, Pattern, Translation, Description string }
	var plan struct {
		Name               string
		NormalizationRules []rule
	}
	rules := []map[string]any{
		{"Name": "Extensions", "Pattern": `^(\d{4})$`, "Translation": "+3314567$1"},
		{"Name": "Operator", "Pattern": `^0$`, "Translation": "+33145670000"},
	}
	const dialPlan = "CsTenantDialPlan"
	out = run("tenant_dial_plan.ps1", obj("Create", dialPlan, "Paris", map[string]any{"NormalizationRules": rules}, "NormalizationRules"))
	if err := json.Unmarshal([]byte(out), &plan); err != nil || plan.Name != "Paris" || len(plan.NormalizationRules) != 2 ||
		plan.NormalizationRules[0].Name != "Extensions" || plan.NormalizationRules[0].Translation != "+3314567$1" {
		t.Errorf("dial plan create output %q: %v", out, err)
	}
	out = run("tenant_dial_plan.ps1", obj("Update", dialPlan, "Paris", map[string]any{"NormalizationRules": rules[1:]}, "NormalizationRules"))
	if err := json.Unmarshal([]byte(out), &plan); err != nil || len(plan.NormalizationRules) != 1 || plan.NormalizationRules[0].Name != "Operator" {
		t.Errorf("dial plan with one rule %q: %v, want a list of one rule", out, err)
	}
	out = run("tenant_dial_plan.ps1", obj("Update", dialPlan, "Paris", map[string]any{"NormalizationRules": []any{}}, "NormalizationRules"))
	if err := json.Unmarshal([]byte(out), &plan); err != nil || len(plan.NormalizationRules) != 0 {
		t.Errorf("dial plan without rules %q: %v", out, err)
	}
	if out := run("tenant_dial_plan.ps1", obj("Get", dialPlan, "Missing", nil, "NormalizationRules")); !IsNotFound([]byte(out)) {
		t.Errorf("Get on a missing dial plan returned %q, want empty output", out)
	}
	run("tenant_dial_plan.ps1", obj("Delete", dialPlan, "Paris", nil))

	// A PSTN usage is an entry of the tenant's single list.
	usage := func(action string) string {
		return run("online_pstn_usage.ps1", map[string]string{"Action": action, "Name": "France"})
	}
	if out := usage("Get"); !IsNotFound([]byte(out)) {
		t.Errorf("Get on a missing usage returned %q", out)
	}
	if out := usage("Create"); out != `{"Name":"France"}` {
		t.Errorf("usage create returned %q", out)
	}
	if err := json.Unmarshal([]byte(run("list.ps1", map[string]string{"Noun": "CsOnlinePstnUsage"})), &ids); err != nil || strings.Join(ids, ",") != "France" {
		t.Errorf("list.ps1 returned %v (%v), want [France]", ids, err)
	}
	usage("Delete")
	usage("Delete")
	if out := usage("Get"); !IsNotFound([]byte(out)) {
		t.Errorf("the usage is still there after Delete: %q", out)
	}
}

// b64JSON encodes v like the Go resources do for the generic scripts.
func b64JSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}
