#!/usr/bin/env bash
# Example: log every PiKVM event to a daily-rotating audit file.
#
# Install:
#   mkdir -p ~/.config/pikvm/hooks.d
#   cp hooks-examples/_all.sh ~/.config/pikvm/hooks.d/
#   chmod +x ~/.config/pikvm/hooks.d/_all.sh
#
# View today's events:
#   tail -f ~/.config/pikvm/audit-$(date +%F).log
set -euo pipefail
LOG="$HOME/.config/pikvm/audit-$(date +%F).log"
mkdir -p "$(dirname "$LOG")"
printf '%s\t%s\t%s@%s\tport=%s\tname=%s\n' \
    "${PIKVM_TIMESTAMP:-?}" \
    "${PIKVM_EVENT:-?}" \
    "${PIKVM_USER:-?}" \
    "${PIKVM_HOST_NAME:-default}" \
    "${PIKVM_PORT:-}" \
    "${PIKVM_NAME:-}" \
    >> "$LOG"
