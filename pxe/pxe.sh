#!/bin/bash

# PXE Boot Management Script
# Unified script for PXE boot setup and management

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Script directory
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
PXE_DIR="$SCRIPT_DIR"
ENV_FILE="$PROJECT_DIR/.env"

# Configuration (loaded from .env)
ISO_PATH=""
UBUNTU_PASSWORD=""
# Default to Raspberry Pi (can be overridden by .env)
PXE_USER="${PXE_USER:-wgr0}"
PXE_HOST="${PXE_HOST:-100.74.180.50}"
PXE_IP="${PXE_IP:-192.168.0.254}"
DHCP_INTERFACE="eth0"
DHCP_RANGE_START="192.168.0.100"
DHCP_RANGE_END="192.168.0.200"
# PXE paths (can be overridden by .env)
PXE_BASE="${PXE_BASE:-/home/wgr0/pxe}"
PXE_TFTP_ROOT="${PXE_TFTP_ROOT:-/home/wgr0/pxe/tftp}"
PXE_HTTP_ROOT="${PXE_HTTP_ROOT:-/home/wgr0/pxe/http}"
NAS_PXE_BASE="/volume1/iPXE"
NAS_TFTP_ROOT="/volume1/iPXE/tftp"
NAS_HTTP_ROOT="/volume1/iPXE/http"

# Load .env if exists
if [ -f "$ENV_FILE" ]; then
    source "$ENV_FILE"
    ISO_PATH="${ISO_PATH:-$PROJECT_DIR/iso/ubuntu-25.10-live-server-amd64.iso}"
    UBUNTU_PASSWORD="${UBUNTU_PASSWORD:-gPZWRkh7Q6@9Pb4vub7!vA}"
fi

# Defaults (use variables set above)
OUTPUT_DIR="$PXE_DIR/files"

# Functions
show_help() {
    cat << EOF
${CYAN}════════════════════════════════════════════════════════════════${NC}
  ${GREEN}PXE Boot Management${NC}
${CYAN}════════════════════════════════════════════════════════════════${NC}

${BLUE}Usage:${NC} ./pxe.sh [COMMAND] [OPTIONS]

${BLUE}Commands:${NC}

  ${GREEN}--extract${NC}              Extract kernel/initrd from ISO
  ${GREEN}--config${NC}                Generate autoinstall config files
  ${GREEN}--setup${NC}                 Generate PXE server setup script
  ${GREEN}--deploy${NC}                Copy files to NAS
  ${GREEN}--test${NC}                  Test PXE configuration
  ${GREEN}--help${NC}                  Show this help

${BLUE}Examples:${NC}

  # Extract kernel/initrd from ISO
  ./pxe.sh --extract

  # Generate autoinstall config
  ./pxe.sh --config

  # Generate PXE server setup script
  ./pxe.sh --setup

  # Copy all files to PXE server
  ./pxe.sh --deploy

  # Full setup (extract + config + deploy)
  ./pxe.sh --extract --config --deploy

${BLUE}Configuration:${NC}
  Edit .env file for:
    - ISO_PATH
    - UBUNTU_PASSWORD
    - PXE_HOST, PXE_USER, PXE_IP

EOF
}

extract_iso() {
    echo ""
    echo -e "${BLUE}[EXTRACT]${NC} Extracting kernel and initrd from ISO..."
    echo ""
    
    if [ ! -f "$ISO_PATH" ]; then
        echo -e "${RED}❌ ISO not found: $ISO_PATH${NC}"
        echo ""
        echo "  Set ISO_PATH in .env or specify:"
        echo "    ISO_PATH=/path/to/iso ./pxe.sh --extract"
        exit 1
    fi
    
    mkdir -p "$OUTPUT_DIR"
    
    echo "  ISO: $ISO_PATH"
    echo "  Output: $OUTPUT_DIR"
    echo ""
    
    # Try different extraction methods
    if command -v hdiutil &> /dev/null; then
        # macOS
        echo "  Using macOS mount method..."
        MOUNT_POINT=$(mktemp -d)
        hdiutil attach "$ISO_PATH" -mountpoint "$MOUNT_POINT" -quiet -nobrowse
        
        if [ -f "$MOUNT_POINT/casper/vmlinuz" ]; then
            cp "$MOUNT_POINT/casper/vmlinuz" "$OUTPUT_DIR/"
            echo -e "${GREEN}✓${NC} Copied vmlinuz"
        fi
        
        if [ -f "$MOUNT_POINT/casper/initrd" ]; then
            cp "$MOUNT_POINT/casper/initrd" "$OUTPUT_DIR/"
            echo -e "${GREEN}✓${NC} Copied initrd"
        fi
        
        hdiutil detach "$MOUNT_POINT" -quiet
        rmdir "$MOUNT_POINT"
        
    elif command -v 7z &> /dev/null; then
        # 7zip
        echo "  Using 7zip extraction..."
        cd "$OUTPUT_DIR"
        7z e "$ISO_PATH" casper/vmlinuz casper/initrd -y &>/dev/null || true
        echo -e "${GREEN}✓${NC} Extracted files"
        
    else
        echo -e "${YELLOW}⚠${NC} No extraction tool found"
        echo ""
        echo "  Install 7zip or extract manually:"
        echo "    mount -o loop $ISO_PATH /mnt/iso"
        echo "    cp /mnt/iso/casper/vmlinuz $OUTPUT_DIR/"
        echo "    cp /mnt/iso/casper/initrd $OUTPUT_DIR/"
        exit 1
    fi
    
    if [ ! -f "$OUTPUT_DIR/vmlinuz" ] || [ ! -f "$OUTPUT_DIR/initrd" ]; then
        echo -e "${RED}❌ Extraction failed - files not found${NC}"
        exit 1
    fi
    
    echo ""
    echo -e "${GREEN}✅ Extraction complete!${NC}"
    echo "  → $OUTPUT_DIR/vmlinuz"
    echo "  → $OUTPUT_DIR/initrd"
    echo ""
}

generate_config() {
    echo ""
    echo -e "${BLUE}[CONFIG]${NC} Generating autoinstall configuration..."
    echo ""
    
    mkdir -p "$OUTPUT_DIR"
    
    # Generate password hash
    if command -v openssl &> /dev/null; then
        PASSWORD_HASH=$(openssl passwd -6 "$UBUNTU_PASSWORD" 2>/dev/null || echo "")
        if [ -z "$PASSWORD_HASH" ]; then
            echo -e "${YELLOW}⚠${NC} Could not generate password hash"
            PASSWORD_HASH="\$6\$rounds=4096\$salt\$CHANGE_ME"
        fi
    else
        echo -e "${YELLOW}⚠${NC} openssl not found, using placeholder"
        PASSWORD_HASH="\$6\$rounds=4096\$salt\$CHANGE_ME"
    fi
    
    # user-data
    cat > "$OUTPUT_DIR/user-data" << USERDATA
#cloud-config
autoinstall:
  version: 1
  identity:
    hostname: minisforum-795s7
    username: llus0
    password: '$PASSWORD_HASH'
    realname: Ubuntu User
  ssh:
    install-server: true
    allow-pw: true
  storage:
    layout:
      name: direct
  packages:
    - git
    - curl
    - wget
    - vim
    - htop
  late-commands:
    - echo "Installation complete!"
USERDATA
    
    # meta-data
    cat > "$OUTPUT_DIR/meta-data" << METADATA
instance-id: minisforum-795s7
local-hostname: minisforum-795s7
METADATA
    
    echo -e "${GREEN}✓${NC} Created user-data"
    echo -e "${GREEN}✓${NC} Created meta-data"
    echo ""
    echo -e "${GREEN}✅ Config files generated!${NC}"
    echo "  → $OUTPUT_DIR/user-data"
    echo "  → $OUTPUT_DIR/meta-data"
    echo ""
}

generate_setup() {
    echo ""
    echo -e "${BLUE}[SETUP]${NC} Generating PXE server setup script..."
    echo ""
    
    cat > "$OUTPUT_DIR/pxe_setup.sh" << PXE_SETUP
#!/bin/bash
# PXE Server Setup - Run this ON the PXE server (Raspberry Pi)
# Usage: bash pxe_setup.sh (some steps need sudo)

set -e

PXE_BASE="${PXE_BASE}"
TFTP_ROOT="${PXE_TFTP_ROOT}"
HTTP_ROOT="${PXE_HTTP_ROOT}"
PXE_SERVER_IP="${PXE_IP}"
DHCP_RANGE_START="${DHCP_RANGE_START}"
DHCP_RANGE_END="${DHCP_RANGE_END}"
DHCP_INTERFACE="${DHCP_INTERFACE}"

echo "════════════════════════════════════════════════════════════════"
echo "  PXE Boot Setup - Raspberry Pi"
echo "════════════════════════════════════════════════════════════════"
echo ""

echo "[1/7] Installing packages (needs sudo)..."
sudo apt update
sudo apt install -y dnsmasq nginx syslinux-common

echo ""
echo "[2/7] Creating directories..."
mkdir -p "\$TFTP_ROOT"
mkdir -p "\$HTTP_ROOT/autoinstall"
mkdir -p "\$TFTP_ROOT/pxelinux.cfg"

echo ""
echo "[3/7] Copying bootloader files..."
sudo cp /usr/lib/syslinux/modules/efi64/ldlinux.e64 "\$TFTP_ROOT/" 2>/dev/null || true
sudo cp /usr/lib/syslinux/modules/efi64/libutil.c32 "\$TFTP_ROOT/" 2>/dev/null || true
sudo cp /usr/lib/syslinux/modules/bios/ldlinux.c32 "\$TFTP_ROOT/" 2>/dev/null || true
sudo cp /usr/lib/syslinux/modules/bios/menu.c32 "\$TFTP_ROOT/" 2>/dev/null || true
sudo cp /usr/lib/syslinux/modules/bios/libutil.c32 "\$TFTP_ROOT/" 2>/dev/null || true

# Set permissions
USER_NAME=\$(whoami)
sudo chmod -R 755 "\$PXE_BASE" 2>/dev/null || true
sudo chown -R \$USER_NAME:\$USER_NAME "\$PXE_BASE" 2>/dev/null || true

echo ""
echo "[4/7] Creating PXE boot menu..."
cat > "\$TFTP_ROOT/pxelinux.cfg/default" << 'MENU'
DEFAULT menu.c32
PROMPT 0
TIMEOUT 50
ONTIMEOUT ubuntu-autoinstall

MENU TITLE PXE Boot Menu

LABEL ubuntu-autoinstall
  MENU LABEL Ubuntu 25.10 Autoinstall
  KERNEL vmlinuz
  INITRD initrd
  APPEND ip=dhcp autoinstall ds=nocloud-net;s=http://${PXE_IP}/autoinstall/ quiet

LABEL ubuntu-manual
  MENU LABEL Ubuntu 25.10 Manual
  KERNEL vmlinuz
  INITRD initrd
  APPEND ip=dhcp quiet
MENU

echo ""
echo "[5/7] Configuring dnsmasq (needs sudo)..."
sudo tee /etc/dnsmasq.d/pxe.conf > /dev/null << EOF
# PXE Boot Configuration
# Override any default TFTP settings
no-hosts
interface=\${DHCP_INTERFACE}
bind-interfaces
dhcp-range=\${DHCP_RANGE_START},\${DHCP_RANGE_END},12h

# TFTP Configuration
enable-tftp
tftp-root=\${TFTP_ROOT}
tftp-lowercase
tftp-no-blocksize
tftp-secure

# UEFI PXE boot (x86_64)
dhcp-match=set:efi-x86_64,option:client-arch,7
dhcp-match=set:efi-x86_64,option:client-arch,9
dhcp-boot=tag:efi-x86_64,ldlinux.e64,\${PXE_SERVER_IP}

# Legacy BIOS PXE boot
dhcp-match=set:bios,option:client-arch,0
dhcp-boot=tag:bios,ldlinux.c32,\${PXE_SERVER_IP}

# Logging
log-dhcp
log-queries
log-facility=/var/log/dnsmasq.log
EOF

echo ""
echo "[6/7] Setting up nginx (needs sudo)..."
sudo tee /etc/nginx/sites-available/pxe > /dev/null << EOF
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    root \${HTTP_ROOT};
    
    # Disable redirects
    absolute_redirect off;
    
    location / {
        autoindex on;
        try_files \$uri \$uri/ =404;
    }
    
    location /autoinstall/ {
        autoindex on;
        default_type text/plain;
        add_header Content-Type text/plain;
    }
}
EOF

sudo ln -sf /etc/nginx/sites-available/pxe /etc/nginx/sites-enabled/
sudo rm -f /etc/nginx/sites-enabled/default 2>/dev/null || true

echo ""
echo "[7/7] Starting services (needs sudo)..."
sudo systemctl restart dnsmasq
sudo systemctl restart nginx
sudo systemctl enable dnsmasq
sudo systemctl enable nginx

echo ""
echo "════════════════════════════════════════════════════════════════"
echo "✅ PXE server configured!"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "📁 Files location: \$PXE_BASE"
echo "  → TFTP: \$TFTP_ROOT"
echo "  → HTTP: \$HTTP_ROOT"
echo ""
echo "📤 Next: Copy PXE files here:"
echo "  cp vmlinuz \$TFTP_ROOT/"
echo "  cp initrd \$TFTP_ROOT/"
echo "  cp user-data \$HTTP_ROOT/autoinstall/"
echo "  cp meta-data \$HTTP_ROOT/autoinstall/"
echo ""
echo "🔍 Check services:"
echo "  sudo systemctl status dnsmasq"
echo "  sudo systemctl status nginx"
echo ""
PXE_SETUP
    
    chmod +x "$OUTPUT_DIR/pxe_setup.sh"
    
    echo -e "${GREEN}✓${NC} Created PXE server setup script"
    echo ""
    echo -e "${GREEN}✅ Setup script generated!${NC}"
    echo "  → $OUTPUT_DIR/pxe_setup.sh"
    echo ""
}

deploy_files() {
    echo ""
    echo -e "${BLUE}[DEPLOY]${NC} Copying files to PXE server..."
    echo ""
    
    if [ ! -d "$OUTPUT_DIR" ]; then
        echo -e "${RED}❌ Output directory not found: $OUTPUT_DIR${NC}"
        echo "  Run --extract and --config first"
        exit 1
    fi
    
    echo "  PXE Server: ${PXE_USER}@${PXE_HOST}"
    echo "  Destination: ${PXE_BASE}"
    echo ""
    
    # Check required files
    REQUIRED_FILES=("vmlinuz" "initrd" "user-data" "meta-data")
    MISSING=()
    
    for file in "${REQUIRED_FILES[@]}"; do
        if [ ! -f "$OUTPUT_DIR/$file" ]; then
            MISSING+=("$file")
        fi
    done
    
    if [ ${#MISSING[@]} -gt 0 ]; then
        echo -e "${RED}❌ Missing required files:${NC}"
        for file in "${MISSING[@]}"; do
            echo "    - $file"
        done
        echo ""
        echo "  Run: ./pxe.sh --extract --config"
        exit 1
    fi
    
    echo "  Creating PXE directory structure..."
    ssh "${PXE_USER}@${PXE_HOST}" "mkdir -p ${PXE_TFTP_ROOT} ${PXE_HTTP_ROOT}/autoinstall" || {
        echo -e "${RED}❌ Cannot connect to PXE server${NC}"
        exit 1
    }
    
    echo "  Copying kernel and initrd to TFTP..."
    scp "$OUTPUT_DIR/vmlinuz" "$OUTPUT_DIR/initrd" \
        "${PXE_USER}@${PXE_HOST}:${PXE_TFTP_ROOT}/" || {
        echo -e "${RED}❌ File copy failed${NC}"
        exit 1
    }
    
    echo "  Copying autoinstall config to HTTP..."
    scp "$OUTPUT_DIR/user-data" "$OUTPUT_DIR/meta-data" \
        "${PXE_USER}@${PXE_HOST}:${PXE_HTTP_ROOT}/autoinstall/" || {
        echo -e "${RED}❌ Config copy failed${NC}"
        exit 1
    }
    
    if [ -f "$OUTPUT_DIR/pxe_setup.sh" ]; then
        echo "  Copying setup script..."
        scp "$OUTPUT_DIR/pxe_setup.sh" "${PXE_USER}@${PXE_HOST}:${PXE_BASE}/" || true
    fi
    
    echo ""
    echo -e "${GREEN}✅ Files deployed to PXE server!${NC}"
    echo ""
    echo "  Files are now in:"
    echo "    → ${PXE_TFTP_ROOT}/vmlinuz"
    echo "    → ${PXE_TFTP_ROOT}/initrd"
    echo "    → ${PXE_HTTP_ROOT}/autoinstall/user-data"
    echo "    → ${PXE_HTTP_ROOT}/autoinstall/meta-data"
    echo ""
    echo "  If you haven't run setup yet:"
    echo "    ssh ${PXE_USER}@${PXE_HOST}"
    echo "    bash ${PXE_BASE}/pxe_setup.sh"
    echo ""
}

test_setup() {
    echo ""
    echo -e "${BLUE}[TEST]${NC} Testing PXE configuration..."
    echo ""
    
    echo "  Checking files..."
    FILES_OK=true
    
    for file in "vmlinuz" "initrd" "user-data" "meta-data"; do
        if [ -f "$OUTPUT_DIR/$file" ]; then
            SIZE=$(du -h "$OUTPUT_DIR/$file" | cut -f1)
            echo -e "${GREEN}✓${NC} $file ($SIZE)"
        else
            echo -e "${RED}✗${NC} $file (missing)"
            FILES_OK=false
        fi
    done
    
    echo ""
    
    if [ "$FILES_OK" = false ]; then
        echo -e "${YELLOW}⚠${NC} Some files are missing"
        echo "  Run: ./pxe.sh --extract --config"
        echo ""
    fi
    
    echo "  Testing PXE server connection..."
    if ssh -o ConnectTimeout=5 "${PXE_USER}@${PXE_HOST}" "echo 'Connected'" &>/dev/null; then
        echo -e "${GREEN}✓${NC} PXE server reachable"
    else
        echo -e "${RED}✗${NC} Cannot connect to PXE server"
        echo "  Check: ssh ${PXE_USER}@${PXE_HOST}"
        echo ""
    fi
    
    echo ""
    echo -e "${GREEN}✅ Test complete!${NC}"
    echo ""
}

# Main
if [ $# -eq 0 ]; then
    show_help
    exit 0
fi

# Process commands
while [[ $# -gt 0 ]]; do
    case $1 in
        --extract)
            extract_iso
            shift
            ;;
        --config)
            generate_config
            shift
            ;;
        --setup)
            generate_setup
            shift
            ;;
        --deploy)
            deploy_files
            shift
            ;;
        --test)
            test_setup
            shift
            ;;
        --help|-h)
            show_help
            exit 0
            ;;
        *)
            echo -e "${RED}❌ Unknown option: $1${NC}"
            echo ""
            show_help
            exit 1
            ;;
    esac
done

echo ""
echo -e "${GREEN}✅ Done!${NC}"
echo ""

