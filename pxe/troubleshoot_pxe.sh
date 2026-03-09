#!/bin/bash
# PXE Troubleshooting Script - Run on NAS

echo "════════════════════════════════════════════════════════════════"
echo "  PXE Boot Troubleshooting"
echo "════════════════════════════════════════════════════════════════"
echo ""

echo "[1] Checking dnsmasq configuration..."
echo ""
cat /etc/dnsmasq.d/pxe.conf
echo ""

echo "[2] Checking if dnsmasq is reading the config..."
echo ""
sudo dnsmasq --test 2>&1 | grep -i "pxe\|tftp\|error" || echo "No obvious errors"
echo ""

echo "[3] Checking TFTP directory and files..."
echo ""
ls -lah /home/lsy/pxe/tftp/
echo ""

echo "[4] Checking TFTP permissions..."
echo ""
stat /home/lsy/pxe/tftp/ | grep -E "Access|Uid|Gid"
echo ""

echo "[5] Testing TFTP accessibility..."
echo ""
echo "TFTP root: /home/lsy/pxe/tftp"
echo "Bootloader files:"
ls -lh /home/lsy/pxe/tftp/*.e64 /home/lsy/pxe/tftp/*.c32 /home/lsy/pxe/tftp/pxelinux.0 2>/dev/null || echo "Some bootloader files missing"
echo ""

echo "[6] Checking dnsmasq process and listening ports..."
echo ""
sudo netstat -tulpn | grep dnsmasq
echo ""

echo "[7] Recent dnsmasq logs (last 20 lines)..."
echo ""
sudo journalctl -u dnsmasq -n 20 --no-pager | tail -20
echo ""

echo "[8] Checking network interface..."
echo ""
ip addr show eth0 | grep -E "inet|state"
echo ""

echo "[9] Testing HTTP server (autoinstall config)..."
echo ""
curl -s http://192.168.0.222/autoinstall/user-data | head -5 || echo "HTTP not accessible"
echo ""

echo "════════════════════════════════════════════════════════════════"
echo "  Common Issues to Check:"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "1. Is dnsmasq listening on the correct interface?"
echo "2. Are bootloader files in TFTP root?"
echo "3. Are file permissions correct (755)?"
echo "4. Is there a firewall blocking TFTP (port 69)?"
echo "5. Is your router's DHCP conflicting?"
echo ""

