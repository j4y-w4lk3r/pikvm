#!/bin/bash

# PiKVM ISO Manager - All-in-one ISO management tool
# Usage: ./iso.sh [command] [options]

set -e

# Load environment variables
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ENV_FILE="$SCRIPT_DIR/.env"

if [ ! -f "$ENV_FILE" ]; then
    echo "❌ Error: .env file not found at $ENV_FILE"
    exit 1
fi

# Source .env file
source "$ENV_FILE"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Show help
show_help() {
    cat << EOF
╔════════════════════════════════════════════════════════════════╗
║              PiKVM ISO Manager - Help                          ║
╚════════════════════════════════════════════════════════════════╝

Usage: ./iso.sh [command] [options]

Commands:
  --help, -h           Show this help message
  --list, -l           List all ISOs on PiKVM
  --storage, -s        Show storage space info
  --test, -t           Test upload API with 1MB file
  --upload, -u         Upload ISO (auto-detects best method)
  --delete, -d         Delete an ISO from PiKVM
  --scp                Upload ISO via SCP (requires SSH)

Examples:
  ./iso.sh --list                           # List available ISOs
  ./iso.sh --storage                        # Show storage space
  ./iso.sh --test                           # Test API (recommended first)
  ./iso.sh --upload                         # Upload default ISO (auto-detects method)
  ./iso.sh --upload /path/to/large.iso      # Upload large file (uses Python streaming)
  ./iso.sh --delete "test.iso"              # Delete an ISO
  ./iso.sh --scp /path/to/file.iso          # Upload via SCP

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
        echo "   You can now upload the full ISO with: ./iso.sh --upload"
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
        if [ ! -f "$SCRIPT_DIR/upload_iso_builtin.py" ]; then
            echo -e "${RED}✗ Python uploader not found!${NC}"
            echo "  Expected: $SCRIPT_DIR/upload_iso_builtin.py"
            exit 1
        fi
        
        # Use Python streaming uploader for large files
        python3 "$SCRIPT_DIR/upload_iso_builtin.py" "$ISO_FILE"
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
        echo "  1. Verify: ./iso.sh --list"
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
    nohup bash -c "$0 --upload \"$ISO_FILE\"" >> "$LOG_FILE" 2>&1 &
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
        echo "  1. Verify: ./iso.sh --list"
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
        echo "Usage: ./iso.sh --delete \"<iso-name>\""
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

# Main command dispatcher
main() {
    if [ $# -eq 0 ]; then
        show_help
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
            show_help
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
            show_help
            exit 1
            ;;
    esac
}

main "$@"

