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
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
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

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

{
  echo "# PiKVM — generated from Vault (${VAULT_KV_PATH})"
  echo "# Regenerate: ./scripts/env-from-vault.sh"
  echo ""

  # Section headers + keys in a stable order (matches prior hand-written .env)
  echo "# PiKVM Configuration"
  for key in PIKVM_HOST PIKVM_USER PIKVM_PASS PIKVM_ROOT_PASS; do
    val=$(echo "$json" | jq -r --arg k "$key" '.data.data[$k] // empty')
    if [[ -z "$val" || "$val" == "null" ]]; then
      echo "error: missing or empty key in Vault: $key (path: $VAULT_KV_PATH)" >&2
      exit 1
    fi
    printf '%s=%s\n' "$key" "$val"
  done

  echo ""
  echo "# ISO Settings"
  key=ISO_PATH
  val=$(echo "$json" | jq -r --arg k "$key" '.data.data[$k] // empty')
  if [[ -z "$val" || "$val" == "null" ]]; then
    echo "error: missing or empty key in Vault: $key" >&2
    exit 1
  fi
  printf '%s=%s\n' "$key" "$val"

  echo ""
  echo "# Tailscale Configuration"
  key=TAILSCALE_AUTH_KEY
  val=$(echo "$json" | jq -r --arg k "$key" '.data.data[$k] // empty')
  if [[ -z "$val" || "$val" == "null" ]]; then
    echo "error: missing or empty key in Vault: $key" >&2
    exit 1
  fi
  printf '%s=%s\n' "$key" "$val"

  echo ""
  echo "# Ubuntu Server Configuration"
  key=UBUNTU_PASSWORD
  val=$(echo "$json" | jq -r --arg k "$key" '.data.data[$k] // empty')
  if [[ -z "$val" || "$val" == "null" ]]; then
    echo "error: missing or empty key in Vault: $key" >&2
    exit 1
  fi
  printf '%s=%s\n' "$key" "$val"
} >"$tmp"

mv "$tmp" "$OUT_FILE"
trap - EXIT
echo "Wrote $OUT_FILE"
