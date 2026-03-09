#!/usr/bin/env bash
# Test PiKVM H.264 stream in terminal (same pipeline as "View video (mpv)" in the TUI).
# Usage: ./test_stream.sh [port]
#   port: optional 0-based port (0, 1, 2, 3...) to set before streaming. Default: leave current.
# Needs: .env (PIKVM_HOST, PIKVM_USER, PIKVM_PASS), websocat, and ffplay or mpv.

set -e
cd "$(dirname "$0")"

if [[ ! -f .env ]]; then
  echo "Missing .env. Create it with PIKVM_HOST=, PIKVM_USER=, PIKVM_PASS="
  exit 1
fi
set -a
source .env
set +a

for key in PIKVM_HOST PIKVM_USER PIKVM_PASS; do
  if [[ -z ${!key} ]]; then
    echo "Missing $key in .env"
    exit 1
  fi
done

if ! command -v websocat &>/dev/null; then
  echo "websocat not found. Install: brew install websocat"
  exit 1
fi

PLAYER=""
if command -v ffplay &>/dev/null; then
  PLAYER="ffplay"
elif command -v mpv &>/dev/null; then
  PLAYER="mpv"
else
  echo "Need ffplay or mpv. Install: brew install ffmpeg  or  brew install mpv"
  exit 1
fi

# Optional: set active switch port so video matches (e.g. ./test_stream.sh 3)
if [[ -n "$1" && "$1" =~ ^[0-9]+$ ]]; then
  PORT="$1"
  echo "Setting PiKVM active port to $PORT..."
  curl -sS -k -X POST -u "${PIKVM_USER}:${PIKVM_PASS}" \
    "https://${PIKVM_HOST}/api/switch/set_active?port=${PORT}" >/dev/null || true
  sleep 0.5
fi

WS_MEDIA_URL="wss://${PIKVM_HOST}/api/media/ws?video=h264"
KEEPER_URL="wss://${PIKVM_HOST}/api/ws?stream=1"

echo "Starting stream keeper (api/ws?stream=1)..."
websocat -k "$KEEPER_URL" \
  -H "X-KVMD-User: $PIKVM_USER" \
  -H "X-KVMD-Passwd: $PIKVM_PASS" &
KEEPER_PID=$!
trap 'kill $KEEPER_PID 2>/dev/null' EXIT

sleep 2
echo "Connecting to media stream and opening $PLAYER (Ctrl+C to stop)..."
# Live H.264 from PiKVM often has no size in first frames; need larger probe so ffplay doesn't bail out
if [[ "$PLAYER" == "ffplay" ]]; then
  websocat -b -B10000000 -k "$WS_MEDIA_URL" \
    -H "X-KVMD-User: $PIKVM_USER" \
    -H "X-KVMD-Passwd: $PIKVM_PASS" \
  | ffplay -f h264 -framerate 30 -probesize 10M -analyzeduration 5M -fflags nobuffer -flags low_delay -i pipe:0 -window_title PiKVM
else
  websocat -b -B10000000 -k "$WS_MEDIA_URL" \
    -H "X-KVMD-User: $PIKVM_USER" \
    -H "X-KVMD-Passwd: $PIKVM_PASS" \
  | mpv --no-cache --demuxer-lavf-format=h264 --demuxer-lavf-o=probesize=10000000,analyzeduration=5000000 -
fi
