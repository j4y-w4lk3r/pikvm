#!/usr/bin/env bash
# Example: post to Slack whenever any PiKVM port is powered on. Reads
# the webhook URL from the SLACK_WEBHOOK env var (set this in your shell
# rc / 1Password env-injection).
#
# Install:
#   mkdir -p ~/.config/pikvm/hooks.d
#   cp hooks-examples/power-on.sh ~/.config/pikvm/hooks.d/
#   chmod +x ~/.config/pikvm/hooks.d/power-on.sh
#   export SLACK_WEBHOOK='https://hooks.slack.com/services/T.../B.../...'
#
# Test:
#   pikvm hooks test power-on port=2.3 name=vault host_name=lab
set -euo pipefail
[[ -z "${SLACK_WEBHOOK:-}" ]] && { echo "SLACK_WEBHOOK not set" >&2; exit 0; }

NAME="${PIKVM_NAME:-port ${PIKVM_PORT:-?}}"
HOST="${PIKVM_HOST_NAME:-pikvm}"
TEXT=":zap: *${NAME}* on \`${HOST}\` powered ON at ${PIKVM_TIMESTAMP:-now}"

curl -sS -X POST -H 'Content-Type: application/json' \
    --data "$(jq -nc --arg t "$TEXT" '{text: $t}')" \
    "$SLACK_WEBHOOK" >/dev/null
