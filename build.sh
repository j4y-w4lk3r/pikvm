#!/bin/bash

# Build script for PiKVM control tool
# Usage: ./build.sh [--help|--test]

cd "$(dirname "$0")"

show_help() {
    cat << EOF
PiKVM Build Script

Usage: ./build.sh [options]

Options:
  (no args)     Build the pikvm binary
  --help, -h    Show this help
  --test, -t    Test .env configuration

Examples:
  ./build.sh           # Build binary
  ./build.sh --test    # Test .env loading

EOF
}

test_env() {
    echo "🧪 Testing .env configuration..."
    echo ""
    
    if [ ! -f .env ]; then
        echo "❌ .env file not found!"
        exit 1
    fi
    
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

