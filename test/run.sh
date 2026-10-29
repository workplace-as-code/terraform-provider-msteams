#!/usr/bin/env bash
# run.sh — build the provider and run the full integration test cycle against
# a real tenant:
#   build → pre-cleanup → apply → drift check → update → configuration writes →
#   import of every policy → drift check → destroy → deletion check
#
# It creates and deletes one policy of each type (TF-CI-*). The tenant-wide
# configurations and the Global feedback policy are imported: two settings
# are changed and restored to test writes (guest memes, external access
# blocked domains), and destroying them only removes them from state.
#
# Usage:
#   ./test/run.sh
#
# Required env vars (exported, or read from .env at repo root; exported values win):
#   TEAMS_TENANT_ID
#   TEAMS_CLIENT_ID
#   and the credentials of the authentication method (TEAMS_AUTH_METHOD):
#     client_certificate (default): TEAMS_CERTIFICATE_PATH, TEAMS_CERTIFICATE_BASE64 or
#                                   TEAMS_CERTIFICATE_THUMBPRINT, and TEAMS_CERTIFICATE_PASSWORD
#     oidc_github                 : nothing more, inside a GitHub Actions job with id-token: write
#     oidc                        : TEAMS_OIDC_TOKEN or TEAMS_OIDC_TOKEN_FILE_PATH
#
# Optional:
#   TF_VAR_test_group_id    object ID of an existing group, to test group policy assignments
#   TF_DEV_PROVIDER_DIR     folder written in a new ~/.terraformrc (default /tmp). When
#                           ~/.terraformrc already has a dev override for this provider, the
#                           provider is built into the folder it names.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TESTDIR="$ROOT/test/integration"
TERRAFORMRC="$HOME/.terraformrc"
PROVIDER_ADDR="workplace-as-code/msteams"

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

pass() { echo -e "${GREEN}  ✓ $*${NC}"; }
fail() { echo -e "${RED}  ✗ $*${NC}"; exit 1; }
step() { echo -e "\n${YELLOW}==> $*${NC}"; }
info() { echo -e "${CYAN}    $*${NC}"; }

# expect_no_changes <label> [terraform plan args...]
# Fails unless terraform plan reports no changes.
expect_no_changes() {
  local label="$1"; shift
  local out
  out="$(mktemp)"
  set +e
  terraform plan -detailed-exitcode -no-color "$@" > "$out" 2>&1
  local code=$?
  set -e
  case $code in
    0) pass "$label: no changes" ;;
    2) cat "$out"; fail "$label: plan shows changes" ;;
    *) cat "$out"; fail "$label: terraform plan failed (exit $code)" ;;
  esac
  rm -f "$out"
}

# ---------------------------------------------------------------------------
# 1. Load credentials from .env if present (exported variables win)
# ---------------------------------------------------------------------------
if [[ -f "$ROOT/.env" ]]; then
  preset="$(export -p)"
  set -a && source "$ROOT/.env" && set +a
  eval "$preset"
fi

# ---------------------------------------------------------------------------
# 2. Check prerequisites
# ---------------------------------------------------------------------------
step "Checking prerequisites"
for cmd in go terraform pwsh; do
  command -v "$cmd" > /dev/null || fail "$cmd is not installed"
done
for var in TEAMS_TENANT_ID TEAMS_CLIENT_ID; do
  [[ -n "${!var:-}" ]] || fail "$var is not set. Add it to $ROOT/.env or export it."
done
AUTH_METHOD="${TEAMS_AUTH_METHOD:-client_certificate}"
# The provider checks the other methods' settings itself, with clearer messages.
if [[ "$AUTH_METHOD" == "client_certificate" && -z "${TEAMS_CERTIFICATE_PATH:-}" &&
      -z "${TEAMS_CERTIFICATE_BASE64:-}" && -z "${TEAMS_CERTIFICATE_THUMBPRINT:-}" ]]; then
  fail "Set TEAMS_CERTIFICATE_PATH or TEAMS_CERTIFICATE_BASE64 (with TEAMS_CERTIFICATE_PASSWORD), or TEAMS_CERTIFICATE_THUMBPRINT."
fi
if [[ -n "${TEAMS_CERTIFICATE_PATH:-}" && ! -r "$TEAMS_CERTIFICATE_PATH" ]]; then
  fail "TEAMS_CERTIFICATE_PATH=$TEAMS_CERTIFICATE_PATH is not a readable file."
fi
pass "Credentials OK (tenant: $TEAMS_TENANT_ID, auth: $AUTH_METHOD)"

# ---------------------------------------------------------------------------
# 3. Check ~/.terraformrc dev_overrides
# ---------------------------------------------------------------------------
# The provider is built into the folder of its dev override, so Terraform
# always runs the binary built here.
DEV_DIR="$(sed -n "s#^ *\"$PROVIDER_ADDR\" *= *\"\([^\"]*\)\".*#\1#p" "$TERRAFORMRC" 2>/dev/null | head -1 || true)"
if [[ -n "$DEV_DIR" ]]; then
  pass "~/.terraformrc already configured ($DEV_DIR)"
elif grep -q 'dev_overrides' "$TERRAFORMRC" 2>/dev/null; then
  fail "~/.terraformrc has dev_overrides but no \"$PROVIDER_ADDR\" entry. Add one, for example \"$PROVIDER_ADDR\" = \"/tmp\"."
else
  DEV_DIR="${TF_DEV_PROVIDER_DIR:-/tmp}"
  step "Configuring ~/.terraformrc dev_overrides"
  cat > "$TERRAFORMRC" <<RC
provider_installation {
  dev_overrides {
    "$PROVIDER_ADDR" = "$DEV_DIR"
  }
  direct {}
}
RC
  pass "~/.terraformrc created ($DEV_DIR)"
fi
BINARY="$DEV_DIR/terraform-provider-msteams"

# ---------------------------------------------------------------------------
# 4. Build provider binary
# ---------------------------------------------------------------------------
step "Building provider binary → $BINARY"
cd "$ROOT"
go build -o "$BINARY" .
pass "Build OK"

# ---------------------------------------------------------------------------
# Objects created by the test, as "<resource address>=<import id>".
# ---------------------------------------------------------------------------
CREATED=(
  "msteams_messaging_policy.test=TF-CI-Messaging"
  "msteams_messaging_policy.minimal=TF-CI-MessagingMinimal"
  "msteams_meeting_policy.test=TF-CI-Meeting"
  "msteams_calling_policy.test=TF-CI-Calling"
  "msteams_app_setup_policy.test=TF-CI-AppSetup"
  "msteams_channels_policy.test=TF-CI-Channels"
  "msteams_update_management_policy.test=TF-CI-Updates"
  "msteams_feedback_policy.test=TF-CI-Feedback"
  "msteams_events_policy.test=TF-CI-Events"
)
if [[ -n "${TF_VAR_test_group_id:-}" ]]; then
  CREATED=("msteams_group_policy_assignment.test[0]=$TF_VAR_test_group_id/TeamsMeetingPolicy" "${CREATED[@]}")
fi
# Objects imported by main.tf and never destroyed.
KEEP=(
  "msteams_feedback_policy.global"
  "msteams_meeting_configuration.this"
  "msteams_client_configuration.this"
  "msteams_guest_messaging_configuration.this"
  "msteams_guest_meeting_configuration.this"
  "msteams_guest_calling_configuration.this"
  "msteams_tenant_federation_configuration.this"
)

# expect_no_changes <label> [terraform plan args...]
# Fails unless terraform plan reports no changes.
expect_no_changes() {
  local label="$1"; shift
  local out
  out="$(mktemp)"
  set +e
  terraform plan -detailed-exitcode -no-color "$@" > "$out" 2>&1
  local code=$?
  set -e
  case $code in
    0) pass "$label: no changes" ;;
    2) cat "$out"; fail "$label: plan shows changes" ;;
    *) cat "$out"; fail "$label: terraform plan failed (exit $code)" ;;
  esac
  rm -f "$out"
}

# state_value <address> <attribute> prints an attribute from the state.
state_value() {
  local state
  # Capture first: with pipefail, a reader closing the pipe early would make
  # terraform exit with SIGPIPE.
  state="$(terraform state show -no-color "$1")"
  sed -n "s/^ *$2 *= *//p" <<< "$state" | head -1
}

# import_orphans imports every test object that still exists in Teams and
# prints the matching -target arguments.
import_orphans() {
  local spec
  for spec in "${CREATED[@]}"; do
    if terraform import -no-color "${spec%%=*}" "${spec#*=}" > /dev/null 2>&1; then
      echo "-target=${spec%%=*}"
    fi
  done
}

reset_state() { rm -f terraform.tfstate terraform.tfstate.*; }

# ---------------------------------------------------------------------------
# 5. Start from an empty state
# ---------------------------------------------------------------------------
step "Cleaning up previous test state"
cd "$TESTDIR"
rm -rf .terraform .terraform.lock.hcl
reset_state
pass "Clean"

# ---------------------------------------------------------------------------
# 6. Pre-cleanup: delete test objects left over by an interrupted run
# ---------------------------------------------------------------------------
step "Pre-cleanup: removing any orphaned test objects"
mapfile -t ORPHANS < <(import_orphans)
if [[ ${#ORPHANS[@]} -gt 0 ]]; then
  info "${#ORPHANS[@]} orphaned object(s) found, destroying..."
  terraform destroy -auto-approve -no-color "${ORPHANS[@]}" | tail -1
  pass "Orphans destroyed"
else
  pass "No orphaned objects found"
fi
reset_state

# ---------------------------------------------------------------------------
# 7. Apply: create the policies and import the tenant-wide objects
# ---------------------------------------------------------------------------
step "terraform apply"
terraform apply -auto-approve -no-color | tail -1
pass "Apply OK"
expect_no_changes "Drift check after apply"

# ---------------------------------------------------------------------------
# 8. In-place update of every policy
# ---------------------------------------------------------------------------
step "Update test (phase=update)"
terraform apply -auto-approve -no-color -var phase=update | tail -1
[[ "$(state_value msteams_messaging_policy.test allow_giphy)" == "true" ]] || fail "allow_giphy is not true in state after the update"
pass "Update applied"
expect_no_changes "Drift check after update" -var phase=update

# ---------------------------------------------------------------------------
# 9. Writes to tenant-wide configurations, then restore the original values
# ---------------------------------------------------------------------------
step "Configuration write test: guest messaging"
original_memes="$(state_value msteams_guest_messaging_configuration.this allow_memes)"
flipped_memes=$([[ "$original_memes" == "true" ]] && echo false || echo true)
terraform apply -auto-approve -no-color -var phase=update -var "guest_allow_memes=$flipped_memes" | tail -1
[[ "$(state_value msteams_guest_messaging_configuration.this allow_memes)" == "$flipped_memes" ]] || fail "allow_memes was not changed"
terraform apply -auto-approve -no-color -var phase=update -var "guest_allow_memes=$original_memes" | tail -1
pass "allow_memes changed to $flipped_memes and restored to $original_memes"
expect_no_changes "Drift check after restore" -var phase=update

step "Configuration write test: external access blocked domains"
blocked="$(state_value msteams_tenant_federation_configuration.this blocked_domains)"
if [[ "$blocked" == "[]" ]]; then
  terraform apply -auto-approve -no-color -var phase=update -var 'federation_blocked_domains=["tf-ci-test.example"]' | tail -1
  state="$(terraform state show -no-color msteams_tenant_federation_configuration.this)"
  grep -q '"tf-ci-test.example"' <<< "$state" || fail "the blocked domain is not in state"
  terraform apply -auto-approve -no-color -var phase=update -var 'federation_blocked_domains=[]' | tail -1
  [[ "$(state_value msteams_tenant_federation_configuration.this blocked_domains)" == "[]" ]] || fail "blocked domains were not restored"
  pass "Blocked domain added and removed"
  expect_no_changes "Drift check after restore" -var phase=update
else
  info "Skipped: the tenant already blocks domains, the test does not touch them"
fi

# ---------------------------------------------------------------------------
# 10. Import test: every policy exists in Teams but not in state
# ---------------------------------------------------------------------------
step "Import test: every policy"
for spec in "${CREATED[@]}"; do
  addr="${spec%%=*}"
  id="${spec#*=}"
  terraform state rm -no-color "$addr" > /dev/null
  # Custom policies are shown as "Tag:<name>" by Get-Cs*Policy: import one that way.
  [[ "$addr" == "msteams_messaging_policy.test" ]] && id="Tag:$id"
  terraform import -no-color -var phase=update "$addr" "$id" > /dev/null || fail "Import of $addr failed"
done
pass "${#CREATED[@]} resources imported"
expect_no_changes "Drift check after import" -var phase=update

# ---------------------------------------------------------------------------
# 10b. terraform query: list every resource type, generate the configuration
#      of what exists (test objects and built-in ones) and check that it only
#      plans imports. Read-only: the configuration is never applied.
# ---------------------------------------------------------------------------
step "terraform query: generated configuration of the whole tenant"
QUERYDIR="$(mktemp -d)"
cat > "$QUERYDIR/main.tf" <<'HCL'
terraform {
  required_providers {
    msteams = { source = "workplace-as-code/msteams" }
  }
}
provider "msteams" {}
HCL
for doc in "$ROOT"/docs/resources/*.md; do
  name="$(basename "$doc" .md)"
  printf 'list "msteams_%s" "all" {\n  provider = msteams\n}\n\n' "$name" >> "$QUERYDIR/all.tfquery.hcl"
done
(cd "$QUERYDIR" && terraform query -no-color -generate-config-out=generated.tf > query.log 2>&1) ||
  { cat "$QUERYDIR/query.log"; fail "terraform query failed"; }
listed=$(grep -c '^import {' "$QUERYDIR/generated.tf")
plan=$(cd "$QUERYDIR" && terraform plan -no-color 2>&1 | grep '^Plan:' || true)
[[ "$plan" == "Plan: $listed to import, 0 to add, 0 to change, 0 to destroy." ]] ||
  fail "The generated configuration does not match the tenant: ${plan:-plan failed} (see $QUERYDIR)"
rm -rf "$QUERYDIR"
pass "$listed objects listed; their generated configuration only plans imports"

# ---------------------------------------------------------------------------
# 11. Destroy the policies, keep the tenant-wide objects
# ---------------------------------------------------------------------------
step "terraform destroy"
for addr in "${KEEP[@]}"; do
  terraform state rm -no-color "$addr" > /dev/null
done
# The import blocks would bring the tenant-wide objects back into the plan.
terraform destroy -auto-approve -no-color -var phase=update $(printf -- '-target=%s ' "${CREATED[@]%%=*}") | tail -1
pass "Destroy OK"

# ---------------------------------------------------------------------------
# 12. Verify the policies are really gone from Teams
# ---------------------------------------------------------------------------
step "Verifying the test objects no longer exist"
reset_state
mapfile -t LEFT < <(import_orphans)
if [[ ${#LEFT[@]} -gt 0 ]]; then
  terraform destroy -auto-approve -no-color "${LEFT[@]}" > /dev/null
  fail "Still present after destroy (now removed): ${LEFT[*]#-target=}"
fi
reset_state
pass "All test objects were deleted"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo -e "${GREEN}╔══════════════════════════════════════╗${NC}"
echo -e "${GREEN}║   All integration tests passed ✓    ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════╝${NC}"
echo ""
echo "  Policies created, updated, imported and destroyed: ${#CREATED[@]}"
echo "  Imported: Global feedback policy and 6 tenant-wide configurations"
echo "  Writes restored: guest messaging, external access blocked domains"
if [[ -z "${TF_VAR_test_group_id:-}" ]]; then
  echo "  Skipped:  group_policy_assignment (set TF_VAR_test_group_id to test it)"
fi
