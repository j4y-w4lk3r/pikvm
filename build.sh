#!/bin/bash

# Build script for PiKVM control tool
# Usage: ./build.sh [--help|--test]

cd "$(dirname "$0")"

show_help() {
    cat << EOF
PiKVM Build Script

Usage: ./build.sh [options]

Options:
  (no args)     Ensure .env (from Vault if missing), then build the pikvm binary
  --help, -h    Show this help
  --test, -t    Test .env configuration

First-time / new machine:
  If .env is missing, this script runs scripts/env-from-vault.sh when possible.
  You need: vault CLI, jq, VAULT_ADDR, and Vault auth (VAULT_TOKEN or vault login).

  Example:
    export VAULT_ADDR='http://100.92.253.124:8200'
    vault login
    ./build.sh

  Optional: VAULT_KV_PATH (default: secret/pikvm)

Examples:
  ./build.sh           # Build binary
  ./build.sh --test    # Test .env loading

EOF
}

# If .env is missing, pull it from HashiCorp Vault KV (see scripts/env-from-vault.sh).
ensure_env() {
    if [ -f .env ]; then
        return 0
    fi

    echo "📋 No .env found; trying HashiCorp Vault (scripts/env-from-vault.sh)..."

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

    if ! ./scripts/env-from-vault.sh; then
        exit 1
    fi
    echo ""
}

test_env() {
    ensure_env
    echo "🧪 Testing .env configuration..."
    echo ""
    
    echo "✅ .env file exists"
    
    # Test pikvm binary
    if [ -f ./pikvm ]; then
        if ./pikvm help > /dev/null 2>&1; then
            echo "✅ pikvm binary loads .env correctly"
        else
            echo "❌ pikvm binary failed"
        fi
    else
        echo "⚠️  pikvm binary not built yet (run ./build.sh)"
    fi
    
    # Test iso.sh
    if ./iso.sh --help > /dev/null 2>&1; then
        echo "✅ iso.sh loads .env correctly"
    else
        echo "❌ iso.sh failed"
    fi
    
    echo ""
    echo "✅ Configuration test complete!"
}

build() {
    ensure_env

    echo "🔧 Building PiKVM control tool..."
    
    # Initialize go module if needed
    if [ ! -f "go.sum" ]; then
        echo "📦 Downloading dependencies..."
        go mod download
    fi
    
    # Build the binary
    echo "🔨 Compiling..."
    go build -o pikvm pikvm.go
    
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

# Main
case "${1:-}" in
    --help|-h)
        show_help
        ;;
    --test|-t)
        test_env
        ;;
    "")
        build
        ;;
    *)
        echo "❌ Unknown option: $1"
        echo ""
        show_help
        exit 1
        ;;
esac

