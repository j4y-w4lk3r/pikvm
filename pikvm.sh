#!/usr/bin/env bash
#
# PiKVM unified shell tools (former build.sh, iso.sh, test_stream.sh)
#   ./pikvm.sh --build   [--test|--help]
#   ./pikvm.sh --iso     [--list|--upload|...]   (same as old iso.sh)
#   ./pikvm.sh --stream  [port]                  (H.264 test; optional 0-based port)
#   ./pikvm.sh --sequence [script]              (run automation/seq-script JSON via automation/pikvm.py)
#

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"
ME="$SCRIPT_DIR/$(basename "$0")"

# ---------------------------------------------------------------------------
# load_pikvm_config — exports PIKVM_HOST/USER/PASS/etc. into the environment.
#
# Idea #3: prefer config.json (written by env-from-vault.sh) over .env. The
# JSON form has fewer foot-guns (no shell expansion of $-strings, no need
# for quoting) and is the same source-of-truth read by pikvm.go and
# automation/pikvm.py.
#
# Returns 0 if config was loaded, 1 if neither file exists.
# ---------------------------------------------------------------------------
load_pikvm_config() {
    if [ -f "$SCRIPT_DIR/config.json" ] && command -v jq >/dev/null 2>&1; then
        local key val
        for key in PIKVM_HOST PIKVM_USER PIKVM_PASS PIKVM_ROOT_PASS \
                   ISO_PATH TAILSCALE_AUTH_KEY UBUNTU_PASSWORD; do
            val=$(jq -r --arg k "$key" '.[$k] // empty' "$SCRIPT_DIR/config.json")
            if [ -n "$val" ] && [ "$val" != "null" ]; then
                export "$key=$val"
            fi
        done
        return 0
    fi
    if [ -f "$SCRIPT_DIR/.env" ]; then
        # shellcheck source=/dev/null
        set -a
        source "$SCRIPT_DIR/.env"
        set +a
        return 0
    fi
    return 1
}

# =============================================================================
# Build (Go binary) — former build.sh
# =============================================================================

show_build_help() {
    cat << EOF
PiKVM Build

Usage: $ME --build [options]

Options:
  (no extra args)  Ensure .env (Vault if missing), set up .venv, then build ./pikvm
  --help, -h       This help
  --test, -t       Test .env and tools

First-time: if .env is missing, runs automation/scripts/env-from-vault.sh (needs vault, jq, VAULT_ADDR).

Examples:
  $ME --build
  $ME --build --test

EOF
}

ensure_build_env() {
    if [ -f config.json ] || [ -f .env ]; then
        return 0
    fi

    echo "📋 No config.json or .env found; trying HashiCorp Vault (automation/scripts/env-from-vault.sh)..."

    if ! command -v vault >/dev/null 2>&1; then
        echo "❌ vault CLI not found. Install: https://developer.hashicorp.com/vault/install"
        echo "   Or copy/create a .env file in this directory."
        exit 1
    fi
    if ! command -v jq >/dev/null 2>&1; then
        echo "❌ jq not found. Install jq, then re-run."
        exit 1
    fi
    if [ -z "${VAULT_ADDR:-}" ]; then
        echo "❌ VAULT_ADDR is not set."
        echo "   Example: export VAULT_ADDR='http://your-vault-host:8200'"
        exit 1
    fi

    if ! ./automation/scripts/env-from-vault.sh; then
        exit 1
    fi
    echo ""
}

ensure_python_venv() {
    # Only relevant for the Python helper scripts (pikvm.py / automation).
    if [ -d ".venv" ] && [ -x ".venv/bin/python" ]; then
        return 0
    fi

    if [ ! -f "requirements.txt" ]; then
        echo "⚠️  requirements.txt not found; skipping venv setup."
        return 0
    fi

    if ! command -v python3 >/dev/null 2>&1; then
        echo "❌ python3 not found. Install Python 3, then re-run."
        exit 1
    fi

    echo "📦 Creating Python virtualenv (.venv) and installing requirements..."
    python3 -m venv .venv

    if [ ! -x ".venv/bin/pip" ]; then
        echo "❌ .venv/bin/pip missing after venv creation."
        echo "   Try: python3 -m ensurepip --upgrade"
        exit 1
    fi

    .venv/bin/pip install --upgrade pip setuptools wheel
    .venv/bin/pip install -r requirements.txt
}

test_build_env() {
    ensure_build_env
    ensure_python_venv
    echo "🧪 Testing .env configuration..."
    echo ""

    echo "✅ .env file exists"

    if [ -f ./pikvm ]; then
        if ./pikvm help > /dev/null 2>&1; then
            echo "✅ pikvm binary loads .env correctly"
        else
            echo "❌ pikvm binary failed"
        fi
    else
        echo "⚠️  pikvm binary not built yet (run $ME --build)"
    fi

    if "$ME" --iso --help > /dev/null 2>&1; then
        echo "✅ pikvm.sh --iso loads .env correctly"
    else
        echo "❌ pikvm.sh --iso failed"
    fi

    echo ""
    echo "✅ Configuration test complete!"
}

build_go_binary() {
    ensure_build_env
    ensure_python_venv

    echo "🔧 Building PiKVM control tool..."

    if [ ! -f "go.sum" ]; then
        echo "📦 Downloading dependencies..."
        go mod download
    fi

    echo "🔨 Compiling..."
    go build -o pikvm ./cmd/pikvm

    if [ $? -eq 0 ]; then
        chmod +x pikvm
        echo "✅ Build successful!"
        echo ""
        echo "Run with: ./pikvm"
        echo "Or install to PATH: sudo cp pikvm /usr/local/bin/"
    else
        echo "❌ Build failed"
        exit 1
    fi
}

build_dispatch() {
    case "${1:-}" in
        --help|-h)
            show_build_help
            ;;
        --test|-t)
            test_build_env
            ;;
        "")
            build_go_binary
            ;;
        *)
            echo "❌ Unknown --build option: $1"
            echo ""
            show_build_help
            exit 1
            ;;
    esac
}

# =============================================================================
# Automation sequences (Python automation/pikvm.py run-sequence)
# =============================================================================

show_sequence_help() {
    cat << EOF
PiKVM Automation Sequences

Usage: $ME --sequence [script]

Options:
  (no extra args)  Run default automation/seq-script/port2-alarm.json
  <script>         Path to sequence JSON (relative to repo root), e.g. automation/seq-script/port2-alarm.json

This wraps:
  .venv/bin/python automation/pikvm.py run-sequence <script> [extra-args]

The Python helper uses .env from the repo root and images under automation/images/.

Examples:
  $ME --sequence
  $ME --sequence automation/seq-script/port2-alarm.json
  $ME --sequence automation/seq-script/port2-alarm.json --verbose

EOF
}

sequence_main() {
    ensure_build_env
    ensure_python_venv

    local script="automation/seq-script/port2-alarm.json"
    if [ $# -gt 0 ]; then
        script="$1"
        shift
    fi

    if [ ! -f "$script" ]; then
        echo "❌ Sequence file not found: $script"
        exit 1
    fi

    if [ ! -x ".venv/bin/python" ]; then
        echo "❌ .venv/bin/python not found. Run: $ME --build"
        exit 1
    fi

    echo "🔁 Running PiKVM sequence via automation/pikvm.py"
    echo "    Script: $script"
    echo ""
    .venv/bin/python automation/pikvm.py run-sequence "$script" "$@"
}

# =============================================================================
# Video stream test — former test_stream.sh
# =============================================================================

stream_main() {
    if ! load_pikvm_config; then
        echo "Missing config: need either config.json or .env"
        echo "Run: ./automation/scripts/env-from-vault.sh"
        exit 1
    fi
    for key in PIKVM_HOST PIKVM_USER PIKVM_PASS; do
        if [[ -z ${!key} ]]; then
            echo "Missing $key in config"
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
}

# =============================================================================
# ISO manager — former iso.sh (.env sourced inside iso_main)
# =============================================================================

ENV_FILE="$SCRIPT_DIR/.env"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# ISO help
iso_show_help() {
    cat << EOF
╔════════════════════════════════════════════════════════════════╗
║              PiKVM ISO Manager - Help                          ║
╚════════════════════════════════════════════════════════════════╝

Usage: ./pikvm.sh --iso [command] [options]

Commands:
  --help, -h           Show this help message
  --list, -l           List all ISOs on PiKVM
  --storage, -s        Show storage space info
  --test, -t           Test upload API with 1MB file
  --upload, -u         Upload ISO (auto-detects best method)
  --delete, -d         Delete an ISO from PiKVM
  --scp                Upload ISO via SCP (requires SSH)

Examples:
  ./pikvm.sh --iso --list                           # List available ISOs
  ./pikvm.sh --iso --storage                        # Show storage space
  ./pikvm.sh --iso --test                           # Test API (recommended first)
  ./pikvm.sh --iso --upload                         # Upload default ISO (auto-detects method)
  ./pikvm.sh --iso --upload /path/to/large.iso      # Upload large file (uses Python streaming)
  ./pikvm.sh --iso --delete "test.iso"              # Delete an ISO
  ./pikvm.sh --iso --scp /path/to/file.iso          # Upload via SCP

Configuration:
  Settings are stored in .env file:
  - PIKVM_HOST      PiKVM IP address
  - PIKVM_USER      Username
  - PIKVM_PASS      Password
  - ISO_PATH        Default ISO to upload

EOF
}

# Show storage info
show_storage() {
    echo "💾 Storage Information"
    echo ""
    
    RESPONSE=$(curl -k -s -u "$PIKVM_USER:$PIKVM_PASS" \
        "https://$PIKVM_HOST/api/msd")
    
    # Extract storage info
    TOTAL=$(echo "$RESPONSE" | python3 -c "import sys, json; data = json.load(sys.stdin); print(data['result']['storage']['parts']['']['size'])" 2>/dev/null || echo "0")
    FREE=$(echo "$RESPONSE" | python3 -c "import sys, json; data = json.load(sys.stdin); print(data['result']['storage']['parts']['']['free'])" 2>/dev/null || echo "0")
    WRITABLE=$(echo "$RESPONSE" | python3 -c "import sys, json; data = json.load(sys.stdin); print(data['result']['storage']['parts']['']['writable'])" 2>/dev/null || echo "false")
    
    TOTAL_GB=$((TOTAL / 1024 / 1024 / 1024))
    FREE_GB=$((FREE / 1024 / 1024 / 1024))
    USED=$((TOTAL - FREE))
    USED_GB=$((USED / 1024 / 1024 / 1024))
    
    PERCENT_USED=$((USED * 100 / TOTAL))
    
    echo "  Total:     ${TOTAL_GB} GB"
    echo "  Used:      ${USED_GB} GB (${PERCENT_USED}%)"
    echo "  Free:      ${FREE_GB} GB"
    echo "  Writable:  $WRITABLE"
    echo ""
}

# List ISOs on PiKVM
list_isos() {
    echo "📋 Available ISOs on PiKVM"
    echo ""
    
    RESPONSE=$(curl -k -s -u "$PIKVM_USER:$PIKVM_PASS" \
        "https://$PIKVM_HOST/api/msd")
    
    # Extract image list
    echo "$RESPONSE" | python3 -c "
import sys, json
data = json.load(sys.stdin)
images = data['result']['storage']['images']

if not images:
    print('  No ISOs found')
else:
    for name, info in images.items():
        size_mb = info['size'] // 1024 // 1024
        complete = '✓' if info['complete'] else '⏳'
        print(f'  {complete} {name} ({size_mb} MB)')
" 2>/dev/null || echo "  Error parsing images"
    
    echo ""
    show_storage
}

# Test upload API
test_api() {
    echo "🧪 Testing PiKVM MSD Upload API..."
    echo ""
    
    # Create test file
    TEST_FILE="/tmp/test_upload_pikvm.iso"
    echo "→ Creating 1MB test file..."
    dd if=/dev/zero of="$TEST_FILE" bs=1M count=1 2>/dev/null
    echo -e "${GREEN}✓${NC} Test file created"
    echo ""
    
    # Test upload
    echo "→ Testing upload..."
    RESPONSE=$(curl -k -s \
        -u "$PIKVM_USER:$PIKVM_PASS" \
        -X POST \
        --data-binary "@$TEST_FILE" \
        "https://$PIKVM_HOST/api/msd/write?image=test_upload_pikvm.iso")
    
    echo "$RESPONSE" | python3 -m json.tool
    echo ""
    
    # Cleanup
    rm -f "$TEST_FILE"
    
    # Check result
    if echo "$RESPONSE" | grep -q '"ok": true'; then
        echo -e "${GREEN}✅ SUCCESS! The API works!${NC}"
        echo "   You can now upload the full ISO with: ./pikvm.sh --iso --upload"
        return 0
    else
        echo -e "${RED}❌ Test failed${NC}"
        return 1
    fi
}

# Upload ISO
upload_iso() {
    local ISO_FILE="${1:-$ISO_PATH}"
    local BACKGROUND="${2:-false}"
    
    echo ""
    echo "════════════════════════════════════════════════════════════════"
    echo "  PiKVM ISO Upload"
    echo "════════════════════════════════════════════════════════════════"
    echo ""
    
    # Step 1: Validate
    echo -e "${BLUE}[STEP 1/6]${NC} Validating prerequisites..."
    
    if [ ! -f "$ISO_FILE" ]; then
        echo -e "  ${RED}✗ ISO file not found: $ISO_FILE${NC}"
        exit 1
    fi
    echo -e "  ${GREEN}✓${NC} ISO file found"
    
    FILE_SIZE=$(stat -f%z "$ISO_FILE" 2>/dev/null || stat -c%s "$ISO_FILE" 2>/dev/null)
    FILE_SIZE_MB=$((FILE_SIZE / 1024 / 1024))
    FILENAME=$(basename "$ISO_FILE")
    
    echo "  → File: $FILENAME"
    echo "  → Size: ${FILE_SIZE_MB} MB"
    
    # Auto-detect best upload method based on file size
    if [ "$FILE_SIZE_MB" -gt 1000 ]; then
        echo ""
        echo "════════════════════════════════════════════════════════════════"
        echo -e "${BLUE}  📊 LARGE FILE DETECTED (${FILE_SIZE_MB} MB)${NC}"
        echo "════════════════════════════════════════════════════════════════"
        echo ""
        echo "  🐍 Using Python streaming uploader (handles large files)"
        echo "  → No memory limits"
        echo "  → Progress tracking every 5%"
        echo "  → Fully scriptable"
        echo ""
        echo "════════════════════════════════════════════════════════════════"
        echo ""
        
        # Check if Python uploader exists
        SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
        PY="$SCRIPT_DIR/automation/pikvm.py"
        if [ ! -f "$PY" ]; then
            echo -e "${RED}✗ Python uploader not found!${NC}"
            echo "  Expected: $PY"
            exit 1
        fi
        
        # Use Python streaming uploader for large files (prefer venv if present)
        if [ -x "$SCRIPT_DIR/.venv/bin/python" ]; then
            "$SCRIPT_DIR/.venv/bin/python" "$PY" upload-builtin "$ISO_FILE"
        else
            python3 "$PY" upload-builtin "$ISO_FILE"
        fi
        exit $?
    fi
    
    # For smaller files, show warning but proceed with API
    if [ "$FILE_SIZE_MB" -gt 500 ]; then
        echo ""
        echo -e "${YELLOW}  ⚠️  Medium-sized file (${FILE_SIZE_MB} MB)${NC}"
        echo "  → Using API upload (may be slow)"
        echo "  → For files >1GB, Python streaming is used automatically"
        echo ""
    fi
    echo ""
    
    # Step 2: Connectivity
    echo -e "${BLUE}[STEP 2/6]${NC} Testing connectivity..."
    if curl -k -s --max-time 5 "https://$PIKVM_HOST/api/info" > /dev/null 2>&1; then
        echo -e "  ${GREEN}✓${NC} PiKVM reachable"
    else
        echo -e "  ${RED}✗${NC} Cannot reach PiKVM"
        exit 1
    fi
    echo ""
    
    # Step 3: Authentication
    echo -e "${BLUE}[STEP 3/6]${NC} Testing authentication..."
    AUTH=$(curl -k -s -u "$PIKVM_USER:$PIKVM_PASS" "https://$PIKVM_HOST/api/auth/check")
    if echo "$AUTH" | grep -q '"ok": true'; then
        echo -e "  ${GREEN}✓${NC} Authentication successful"
    else
        echo -e "  ${RED}✗${NC} Authentication failed"
        exit 1
    fi
    echo ""
    
    # Step 4: Check space
    echo -e "${BLUE}[STEP 4/6]${NC} Checking storage space..."
    MSD_STATUS=$(curl -k -s -u "$PIKVM_USER:$PIKVM_PASS" "https://$PIKVM_HOST/api/msd")
    FREE_SPACE=$(echo "$MSD_STATUS" | python3 -c "import sys, json; data = json.load(sys.stdin); print(data['result']['storage']['parts']['']['free'])" 2>/dev/null)
    FREE_SPACE_MB=$((FREE_SPACE / 1024 / 1024))
    
    echo "  → Free space: ${FREE_SPACE_MB} MB"
    
    if [ "$FREE_SPACE" -lt "$FILE_SIZE" ]; then
        echo -e "  ${RED}✗${NC} Not enough space!"
        exit 1
    fi
    echo -e "  ${GREEN}✓${NC} Sufficient space"
    echo ""
    
    # Step 5: Prepare MSD
    echo -e "${BLUE}[STEP 5/6]${NC} Preparing MSD..."
    curl -k -s -u "$PIKVM_USER:$PIKVM_PASS" \
        -X POST -H "Content-Type: application/json" \
        -d '{"connected": false}' \
        "https://$PIKVM_HOST/api/msd/set_params" > /dev/null
    
    curl -k -s -u "$PIKVM_USER:$PIKVM_PASS" \
        -X POST -H "Content-Type: application/json" \
        -d '{"image": null}' \
        "https://$PIKVM_HOST/api/msd/set_params" > /dev/null
    echo -e "  ${GREEN}✓${NC} MSD prepared"
    echo ""
    
    # Step 6: Upload
    echo -e "${BLUE}[STEP 6/6]${NC} Uploading ISO..."
    echo -e "${YELLOW}  ⏳ Uploading ${FILE_SIZE_MB} MB - This will take several minutes...${NC}"
    echo "  → Started at: $(date '+%H:%M:%S')"
    echo "  → File: $ISO_FILE"
    echo ""
    
    # Verify file exists and is readable
    if [ ! -r "$ISO_FILE" ]; then
        echo -e "${RED}  ✗ Cannot read file: $ISO_FILE${NC}"
        exit 1
    fi
    
    # Start background upload and capture response
    TEMP_RESPONSE=$(mktemp)
    TEMP_ERROR=$(mktemp)
    START_TIME=$(date +%s)
    
    echo "  🚀 Starting upload process..."
    (
        # Note: For files >1GB, curl may run out of memory
        # Use the web interface or SCP method for large files
        curl -k -v \
            -u "$PIKVM_USER:$PIKVM_PASS" \
            -X POST \
            --data-binary "@$ISO_FILE" \
            "https://$PIKVM_HOST/api/msd/write?image=$FILENAME" \
            > "$TEMP_RESPONSE" 2> "$TEMP_ERROR"
    ) &
    CURL_PID=$!
    
    echo "  → Upload PID: $CURL_PID"
    
    # Wait 3 seconds to ensure upload started
    sleep 3
    
    # Check if process is still running
    if ! ps -p $CURL_PID > /dev/null 2>&1; then
        echo ""
        echo -e "${RED}  ✗ Upload process died immediately!${NC}"
        echo ""
        echo "  Error details:"
        cat "$TEMP_ERROR"
        echo ""
        echo "  Response:"
        cat "$TEMP_RESPONSE"
        rm -f "$TEMP_RESPONSE" "$TEMP_ERROR"
        exit 1
    fi
    
    echo "  📊 Upload Progress (live monitoring):"
    echo "  ─────────────────────────────────────────────────────────────"
    echo ""
    
    # Monitor progress continuously
    DOTS=""
    COUNTER=0
    while ps -p $CURL_PID > /dev/null 2>&1; do
        COUNTER=$((COUNTER + 1))
        CURRENT_TIME=$(date +%s)
        ELAPSED=$((CURRENT_TIME - START_TIME))
        
        # Format elapsed time
        HOURS=$((ELAPSED / 3600))
        MINUTES=$(((ELAPSED % 3600) / 60))
        SECONDS=$((ELAPSED % 60))
        
        # Add animated dots (max 3)
        DOTS="${DOTS}."
        if [ ${#DOTS} -gt 3 ]; then
            DOTS="."
        fi
        
        # Calculate estimated time (rough estimate based on typical 2MB/s)
        ESTIMATED_TOTAL=$((FILE_SIZE_MB / 2))  # seconds at 2MB/s
        REMAINING=$((ESTIMATED_TOTAL - ELAPSED))
        if [ $REMAINING -lt 0 ]; then
            REMAINING=0
        fi
        REM_MIN=$((REMAINING / 60))
        REM_SEC=$((REMAINING % 60))
        
        # Show progress line (overwrite with \r)
        printf "\r  ⏱️  Elapsed: %02d:%02d:%02d | Est. remaining: ~%02dm %02ds | Status: Uploading%-3s" \
            $HOURS $MINUTES $SECONDS $REM_MIN $REM_SEC "$DOTS"
        
        sleep 2
    done
    
    # Wait for completion
    wait $CURL_PID
    CURL_EXIT=$?
    
    RESPONSE=$(cat "$TEMP_RESPONSE")
    
    # Check if curl failed
    if [ $CURL_EXIT -ne 0 ]; then
        echo ""
        echo ""
        echo -e "${RED}  ✗ Upload failed (curl exit code: $CURL_EXIT)${NC}"
        echo ""
        echo "  Error log:"
        cat "$TEMP_ERROR" | tail -20
        rm -f "$TEMP_RESPONSE" "$TEMP_ERROR"
        exit 1
    fi
    
    rm -f "$TEMP_RESPONSE" "$TEMP_ERROR"
    
    # Calculate final time
    END_TIME=$(date +%s)
    TOTAL_ELAPSED=$((END_TIME - START_TIME))
    TOTAL_MIN=$((TOTAL_ELAPSED / 60))
    TOTAL_SEC=$((TOTAL_ELAPSED % 60))
    AVG_SPEED=$((FILE_SIZE_MB / TOTAL_ELAPSED))
    
    echo ""
    echo ""
    echo "  ─────────────────────────────────────────────────────────────"
    echo "  → Finished at: $(date '+%H:%M:%S')"
    echo "  → Total time: ${TOTAL_MIN}m ${TOTAL_SEC}s"
    echo "  → Average speed: ~${AVG_SPEED} MB/s"
    echo ""
    
    # Check result
    if echo "$RESPONSE" | grep -q '"ok": true'; then
        echo "════════════════════════════════════════════════════════════════"
        echo -e "  ${GREEN}✓✓✓ SUCCESS! ISO UPLOADED! ✓✓✓${NC}"
        echo "════════════════════════════════════════════════════════════════"
        echo ""
        echo "  Next steps:"
        echo "  1. Verify: ./pikvm.sh --iso --list"
        echo "  2. Boot: ./pikvm"
        echo "  3. Select: [1] Boot Ubuntu ISO"
        echo ""
    else
        echo -e "${RED}❌ Upload failed${NC}"
        echo "$RESPONSE"
        exit 1
    fi
}

# Upload in background
upload_background() {
    local ISO_FILE="${1:-$ISO_PATH}"
    local LOG_FILE="$SCRIPT_DIR/upload_progress.log"
    local PID_FILE="$SCRIPT_DIR/upload.pid"
    
    # Check if already running
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if ps -p "$PID" > /dev/null 2>&1; then
            echo "⚠️  Upload already in progress (PID: $PID)"
            echo "   Monitor: tail -f $LOG_FILE"
            exit 1
        else
            rm -f "$PID_FILE"
        fi
    fi
    
    echo "🚀 Starting background upload..."
    echo "   Log file: $LOG_FILE"
    echo ""
    
    > "$LOG_FILE"
    
    # Run in background
    nohup bash -c "\"$ME\" --iso --upload \"$ISO_FILE\"" >> "$LOG_FILE" 2>&1 &
    UPLOAD_PID=$!
    
    echo "$UPLOAD_PID" > "$PID_FILE"
    
    echo -e "${GREEN}✅ Upload started (PID: $UPLOAD_PID)${NC}"
    echo ""
    echo "Commands:"
    echo "  Monitor: tail -f $LOG_FILE"
    echo "  Status:  ps -p $UPLOAD_PID"
    echo "  Kill:    kill $UPLOAD_PID"
    echo ""
}

# Upload via SCP
upload_scp() {
    local ISO_FILE="${1:-$ISO_PATH}"
    local REMOTE_PATH="/var/lib/kvmd/msd"
    
    echo "📀 Uploading ISO via SCP..."
    echo ""
    
    if [ ! -f "$ISO_FILE" ]; then
        echo -e "${RED}✗ ISO file not found: $ISO_FILE${NC}"
        exit 1
    fi
    
    FILE_SIZE=$(stat -f%z "$ISO_FILE" 2>/dev/null || stat -c%s "$ISO_FILE" 2>/dev/null)
    FILE_SIZE_MB=$((FILE_SIZE / 1024 / 1024))
    FILENAME=$(basename "$ISO_FILE")
    
    echo "  File: $FILENAME"
    echo "  Size: ${FILE_SIZE_MB} MB"
    echo ""
    
    # Check if we have the root password
    if [ -z "$PIKVM_ROOT_PASS" ]; then
        echo -e "${YELLOW}⚠️  PIKVM_ROOT_PASS not set in .env${NC}"
        echo "  You'll be prompted for SSH password..."
        echo ""
        scp "$ISO_FILE" "root@${PIKVM_HOST}:${REMOTE_PATH}/"
    else
        # Check if sshpass is installed
        if command -v sshpass &> /dev/null; then
            echo "🔐 Using SSH password from .env..."
            echo "🔓 Remounting PiKVM filesystem as read-write..."
            sshpass -p "$PIKVM_ROOT_PASS" ssh -o StrictHostKeyChecking=no "root@${PIKVM_HOST}" "rw" 2>/dev/null
            echo "⏳ Uploading ${FILE_SIZE_MB} MB (this will take several minutes)..."
            echo ""
            sshpass -p "$PIKVM_ROOT_PASS" scp -o StrictHostKeyChecking=no "$ISO_FILE" "root@${PIKVM_HOST}:${REMOTE_PATH}/"
            SCP_EXIT=$?
            echo ""
            echo "🔒 Remounting PiKVM filesystem as read-only..."
            sshpass -p "$PIKVM_ROOT_PASS" ssh -o StrictHostKeyChecking=no "root@${PIKVM_HOST}" "ro" 2>/dev/null
            return $SCP_EXIT
        else
            echo -e "${YELLOW}⚠️  sshpass not installed${NC}"
            echo "  Install with: brew install hudochenkov/sshpass/sshpass"
            echo "  Or enter SSH password manually when prompted..."
            echo ""
            scp "$ISO_FILE" "root@${PIKVM_HOST}:${REMOTE_PATH}/"
        fi
    fi
    
    SCP_EXIT=$?
    
    if [ $SCP_EXIT -eq 0 ]; then
        echo ""
        echo -e "${GREEN}✅ Upload successful!${NC}"
        echo ""
        echo "  Next steps:"
        echo "  1. Verify: ./pikvm.sh --iso --list"
        echo "  2. Boot: ./pikvm"
        echo "  3. Select: [1] Boot Ubuntu ISO"
        echo ""
    else
        echo ""
        echo -e "${RED}❌ Upload failed (exit code: $SCP_EXIT)${NC}"
        exit 1
    fi
}

# Delete ISO
delete_iso() {
    local ISO_NAME="$1"
    
    if [ -z "$ISO_NAME" ]; then
        echo "❌ Error: ISO name required"
        echo "Usage: ./pikvm.sh --iso --delete \"<iso-name>\""
        echo ""
        echo "Available ISOs:"
        list_isos
        exit 1
    fi
    
    echo "🗑️  Deleting ISO: $ISO_NAME"
    echo ""
    
    # URL-encode the filename (handle spaces and special characters)
    ISO_NAME_ENCODED=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$ISO_NAME'))")
    echo "  → Encoded filename: $ISO_NAME_ENCODED"
    echo ""
    
    # First, disconnect if connected
    curl -k -s -u "$PIKVM_USER:$PIKVM_PASS" \
        -X POST \
        -H "Content-Type: application/json" \
        -d '{"connected": false}' \
        "https://$PIKVM_HOST/api/msd/set_params" > /dev/null
    
    # Try to delete using remove endpoint
    RESPONSE=$(curl -k -s -u "$PIKVM_USER:$PIKVM_PASS" \
        -X POST \
        "https://$PIKVM_HOST/api/msd/remove?image=$ISO_NAME_ENCODED")
    
    echo "$RESPONSE" | python3 -m json.tool
    echo ""
    
    if echo "$RESPONSE" | grep -q '"ok": true'; then
        echo -e "${GREEN}✓ ISO deleted successfully!${NC}"
        echo ""
        echo "Remaining ISOs:"
        list_isos
    else
        echo -e "${RED}❌ Delete failed${NC}"
        echo ""
        echo "Alternative: Delete via web interface:"
        echo "  https://$PIKVM_HOST/kvm/ → System → Mass Storage"
        exit 1
    fi
}

# ISO command dispatcher (requires config.json or .env)
iso_main() {
    if ! load_pikvm_config; then
        echo "❌ Error: no config.json or .env found in $SCRIPT_DIR"
        echo "   Run: ./automation/scripts/env-from-vault.sh"
        exit 1
    fi

    if [ $# -eq 0 ]; then
        iso_show_help
        exit 0
    fi
    
    # Parse arguments
    local COMMAND="$1"
    shift
    
    # For delete command, capture all remaining arguments (handles spaces in filenames)
    local FILE_PATH="$ISO_PATH"
    local DELETE_NAME=""
    
    if [ "$COMMAND" = "--delete" ] || [ "$COMMAND" = "-d" ]; then
        # Capture all remaining arguments as the ISO name (handles spaces)
        DELETE_NAME="$*"
    else
        # Check for --file flag for other commands
        if [ "$1" = "--file" ] || [ "$1" = "-f" ]; then
            if [ -z "$2" ]; then
                echo "❌ Error: --file requires a path argument"
                exit 1
            fi
            FILE_PATH="$2"
        elif [ -n "$1" ] && [ "$1" != "--file" ] && [ "$1" != "-f" ]; then
            # If there's an argument and it's not --file, treat it as the file path
            FILE_PATH="$1"
        fi
    fi
    
    case "$COMMAND" in
        --help|-h)
            iso_show_help
            ;;
        --list|-l)
            list_isos
            ;;
        --storage|-s)
            show_storage
            ;;
        --test|-t)
            test_api
            ;;
        --upload|-u)
            upload_iso "$FILE_PATH"
            ;;
        --background|-b)
            upload_background "$FILE_PATH"
            ;;
        --delete|-d)
            delete_iso "$DELETE_NAME"
            ;;
        --scp)
            upload_scp "$FILE_PATH"
            ;;
        *)
            echo "❌ Unknown command: $COMMAND"
            echo ""
            iso_show_help
            exit 1
            ;;
    esac
}

# =============================================================================
# Top-level router
# =============================================================================

show_main_help() {
    cat << EOF
PiKVM shell tools

Usage:

  $ME --build [--test|--help]        Build Go ./pikvm binary (pulls .env from Vault if missing)
  $ME --iso [--list|--upload|...]    ISO / MSD manager (same commands as old iso.sh)
  $ME --stream [port]                Test H.264 stream in ffplay/mpv (optional port 0..n)
  $ME --sequence [script]            Run Python automation sequence (automation/pikvm.py run-sequence)

Examples:

  $ME --build
  $ME --build --test
  $ME --iso --list
  $ME --iso --upload /path/to/file.iso
  $ME --stream
  $ME --stream 2

Python automation (wait for screen image, then type):

  # From repo root (no manual venv activation needed):
  $ME --sequence                       # runs default automation/seq-script/port2-alarm.json
  $ME --sequence automation/seq-script/port2-alarm.json

EOF
}

main() {
    if [ $# -eq 0 ]; then
        show_main_help
        exit 0
    fi

    local cmd="$1"
    shift

    case "$cmd" in
        --build|-b)
            build_dispatch "$@"
            ;;
        --iso|-i)
            iso_main "$@"
            ;;
        --stream|--test-stream)
            stream_main "$@"
            ;;
        --sequence|-q)
            if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
                show_sequence_help
            else
                sequence_main "$@"
            fi
            ;;
        --help|-h)
            show_main_help
            ;;
        *)
            echo "❌ Unknown command: $cmd"
            echo ""
            show_main_help
            exit 1
            ;;
    esac
}

main "$@"

