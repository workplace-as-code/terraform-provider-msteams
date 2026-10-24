#!/usr/bin/env bash
# validate_examples.sh — runs terraform validate on every example against the
# current provider schema. Terraform must reach the provider through a
# dev_overrides entry (see CONTRIBUTING.md).
#
# Resource and data source examples have no terraform or provider block (they
# are rendered in the documentation as is): each one is validated in a
# temporary module that adds the required_providers block and declares the
# variables it uses.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# The provider name comes from the Go module path, not from the folder name.
PROVIDER="$(sed -n 's#^module .*/terraform-provider-##p' "$ROOT/go.mod")"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

failed=0

# validate DIR LABEL
validate() {
  local out
  if ! out="$(cd "$1" && terraform validate -no-color 2>&1)"; then
    echo "::error::$2 is not valid"
    echo "$out"
    failed=1
  fi
}

# module NAME SOURCE_FILES... — builds a module from example files.
module() {
  local mod="$WORK/$1"
  shift
  mkdir -p "$mod"
  cp "$@" "$mod/"
  if ! grep -q '^terraform {' "$mod"/*.tf; then
    printf 'terraform {\n  required_providers {\n    %s = { source = "workplace-as-code/%s" }\n  }\n}\n' \
      "$PROVIDER" "$PROVIDER" > "$mod/providers.tf"
  fi
  for v in $(grep -oh 'var\.[a-z0-9_]*' "$mod"/*.tf | cut -d. -f2 | sort -u); do
    grep -q "^variable \"$v\"" "$mod"/*.tf || echo "variable \"$v\" {}" >> "$mod/variables.tf"
  done
  echo "$mod"
}

for dir in "$ROOT"/examples/resources/*/; do
  validate "$(module "r-$(basename "$dir")" "$dir/resource.tf")" "${dir#"$ROOT"/}"
done
for dir in "$ROOT"/examples/data-sources/*/; do
  [[ -d "$dir" ]] || continue
  validate "$(module "d-$(basename "$dir")" "$dir/data-source.tf")" "${dir#"$ROOT"/}"
done
validate "$(module provider "$ROOT/examples/provider/provider.tf")" examples/provider
validate "$ROOT/examples/full_tenant" examples/full_tenant

exit $failed
