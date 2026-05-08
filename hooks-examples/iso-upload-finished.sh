#!/usr/bin/env bash
# Example: ring the terminal bell + send a macOS notification when a
# PiKVM ISO upload completes. Useful for "drop a 4 GB ISO and walk away."
#
# Install:
#   mkdir -p ~/.config/pikvm/hooks.d
#   cp hooks-examples/iso-upload-finished.sh ~/.config/pikvm/hooks.d/
#   chmod +x ~/.config/pikvm/hooks.d/iso-upload-finished.sh
set -euo pipefail
NAME="${PIKVM_NAME:-}"
HOST="${PIKVM_HOST_NAME:-pikvm}"

# Ring the terminal bell for any TUI / shell that's listening.
printf '\a'

# macOS desktop notification (silently ignored on Linux).
if command -v osascript >/dev/null 2>&1; then
    osascript -e "display notification \"$NAME ready to mount on $HOST\" with title \"PiKVM ISO upload finished\""
fi

# notify-send for Linux desktops (silently ignored if not present).
if command -v notify-send >/dev/null 2>&1; then
    notify-send "PiKVM ISO upload finished" "$NAME ready to mount on $HOST"
fi
