#!/usr/bin/env bash
# Recreate .env from HashiCorp Vault KV v2.
#
# Prerequisites: vault CLI, jq, and a token with read access to the path.
#
# Typical usage (point VAULT_ADDR at your server, e.g. https://j4yn0:8200):
#   export VAULT_ADDR='https://j4yn0:8200'
#   export VAULT_TOKEN='...'   # or: vault login
#   export VAULT_KV_PATH='secret/pikvm'   # optional; default shown
#   ./scripts/env-from-vault.sh
#
# Vault KV v2: store a flat object at that path with these keys (names must match):
#   PIKVM_HOST, PIKVM_USER, PIKVM_PASS, PIKVM_ROOT_PASS,
#   ISO_PATH, TAILSCALE_AUTH_KEY, UBUNTU_PASSWORD
#
# One-time population example:
#   vault kv put secret/pikvm \
#     PIKVM_HOST=... PIKVM_USER=... PIKVM_PASS=... PIKVM_ROOT_PASS=... \
#     ISO_PATH=... TAILSCALE_AUTH_KEY=... UBUNTU_PASSWORD=...

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# This file lives at automation/scripts/ — repo root is two levels up.
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUT_FILE="${1:-$PROJECT_ROOT/.env}"

VAULT_KV_PATH="${VAULT_KV_PATH:-secret/pikvm}"

if ! command -v vault >/dev/null 2>&1; then
  echo "error: vault CLI not found (install: https://developer.hashicorp.com/vault/install)" >&2
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "error: jq not found" >&2
  exit 1
fi

if [[ -z "${VAULT_ADDR:-}" ]]; then
  echo "error: VAULT_ADDR is not set (e.g. export VAULT_ADDR='https://j4yn0:8200')" >&2
  exit 1
fi

json=$(vault kv get -format=json "$VAULT_KV_PATH")

# Cleanup any temp files we leave behind on error.
cleanup() {
  rm -f "${env_tmp:-}" "${config_tmp:-}"
}
trap cleanup EXIT

# Required keys (must be present in Vault). All consumers — pikvm.go,
# pikvm.sh, automation/pikvm.py — read these names.
REQUIRED_KEYS=(
  PIKVM_HOST PIKVM_USER PIKVM_PASS PIKVM_ROOT_PASS
  ISO_PATH
  TAILSCALE_AUTH_KEY
  UBUNTU_PASSWORD
)

# Pull every value once and stash in an associative array so we can write
# both .env and config.json without re-querying Vault.
declare -A VALUES
for key in "${REQUIRED_KEYS[@]}"; do
  val=$(echo "$json" | jq -r --arg k "$key" '.data.data[$k] // empty')
  if [[ -z "$val" || "$val" == "null" ]]; then
    echo "error: missing or empty key in Vault: $key (path: $VAULT_KV_PATH)" >&2
    exit 1
  fi
  VALUES[$key]="$val"
done

# ----- write .env (back-compat for anything that still reads it) ------------

env_tmp="$(mktemp)"
{
  echo "# PiKVM — generated from Vault (${VAULT_KV_PATH})"
  echo "# Regenerate: ./automation/scripts/env-from-vault.sh"
  echo ""
  echo "# PiKVM Configuration"
  for k in PIKVM_HOST PIKVM_USER PIKVM_PASS PIKVM_ROOT_PASS; do
    printf '%s=%s\n' "$k" "${VALUES[$k]}"
  done
  echo ""
  echo "# ISO Settings"
  printf 'ISO_PATH=%s\n' "${VALUES[ISO_PATH]}"
  echo ""
  echo "# Tailscale Configuration"
  printf 'TAILSCALE_AUTH_KEY=%s\n' "${VALUES[TAILSCALE_AUTH_KEY]}"
  echo ""
  echo "# Ubuntu Server Configuration"
  printf 'UBUNTU_PASSWORD=%s\n' "${VALUES[UBUNTU_PASSWORD]}"
} >"$env_tmp"
chmod 600 "$env_tmp"
mv "$env_tmp" "$OUT_FILE"
echo "Wrote $OUT_FILE"

# ----- write config.json (the new single-source-of-truth, idea #3) ----------
#
# Schema: flat object, same keys as .env, plus schema_version. Consumers
# (Go / Bash / Python) try config.json first and fall back to .env.

CONFIG_FILE="$(dirname "$OUT_FILE")/config.json"
config_tmp="$(mktemp)"

# Build with jq so values are properly JSON-escaped (handles passwords with
# quotes, backslashes, etc.).
jq_args=(--arg _schema 1)
jq_filter='{ "schema_version": ($_schema | tonumber)'
for k in "${REQUIRED_KEYS[@]}"; do
  jq_args+=(--arg "$k" "${VALUES[$k]}")
  jq_filter+=", \"$k\": \$$k"
done
jq_filter+=" }"

# shellcheck disable=SC2068
jq -n "${jq_args[@]}" "$jq_filter" >"$config_tmp"
chmod 600 "$config_tmp"
mv "$config_tmp" "$CONFIG_FILE"
echo "Wrote $CONFIG_FILE"
