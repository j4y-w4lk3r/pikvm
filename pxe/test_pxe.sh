#!/bin/bash
# PXE Test Script - Run on NAS to test TFTP and verify setup

echo "════════════════════════════════════════════════════════════════"
echo "  PXE Boot Test & Diagnostics"
echo "════════════════════════════════════════════════════════════════"
echo ""

echo "[1] Checking dnsmasq status..."
systemctl status dnsmasq --no-pager | head -10
echo ""

echo "[2] Checking TFTP files..."
ls -lh /volume1/pxe/tftp/
echo ""

echo "[3] Testing TFTP server locally..."
echo "  Attempting to download ldlinux.e64 via TFTP..."
timeout 5 tftp localhost <<EOF
binary
get ldlinux.e64 /tmp/test_ldlinux.e64
quit
EOF

if [ -f /tmp/test_ldlinux.e64 ]; then
    echo "  ✓ TFTP download successful!"
    rm /tmp/test_ldlinux.e64
else
    echo "  ✗ TFTP download failed"
fi
echo ""

echo "[4] Checking dnsmasq configuration..."
cat /etc/dnsmasq.d/pxe.conf
echo ""

echo "[5] Checking network interface..."
ip addr show eth0 | grep -E "inet|state"
echo ""

echo "[6] Testing HTTP server..."
curl -s http://192.168.0.222/autoinstall/user-data | head -5
echo ""

echo "[7] Recent dnsmasq logs (watch for DHCP requests)..."
echo "  (Boot your MINISFORUM now and watch for DHCP requests)"
echo ""
journalctl -u dnsmasq -n 50 --no-pager | tail -20
echo ""

echo "[8] Checking if dnsmasq is listening on all interfaces..."
netstat -tulpn | grep dnsmasq
echo ""

echo "════════════════════════════════════════════════════════════════"
echo "  Next Steps:"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "1. Boot MINISFORUM with network boot"
echo "2. Watch dnsmasq logs: journalctl -u dnsmasq -f"
echo "3. You should see DHCP requests when booting"
echo "4. If no requests, check network/firewall"
echo ""

