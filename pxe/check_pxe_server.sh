#!/bin/bash
# Comprehensive PXE Server Check - Run on NAS

echo "════════════════════════════════════════════════════════════════"
echo "  PXE Server Diagnostic Check"
echo "════════════════════════════════════════════════════════════════"
echo ""

echo "[1] Service Status"
echo "────────────────────────────────────────────────────────────────"
systemctl status dnsmasq --no-pager | head -5
systemctl status nginx --no-pager | head -5
echo ""

echo "[2] Network Interface"
echo "────────────────────────────────────────────────────────────────"
ip addr show eth0 | grep -E "inet|state"
echo ""

echo "[3] Listening Ports"
echo "────────────────────────────────────────────────────────────────"
echo "DHCP (port 67):"
netstat -tulpn | grep :67
echo ""
echo "TFTP (port 69):"
netstat -tulpn | grep :69
echo ""

echo "[4] Firewall Check"
echo "────────────────────────────────────────────────────────────────"
if command -v iptables &> /dev/null; then
    iptables -L -n | grep -E "67|69" || echo "No firewall rules for ports 67/69"
else
    echo "iptables not found, checking ufw..."
    ufw status 2>/dev/null || echo "No firewall detected"
fi
echo ""

echo "[5] dnsmasq Configuration"
echo "────────────────────────────────────────────────────────────────"
cat /etc/dnsmasq.d/pxe.conf
echo ""

echo "[6] TFTP Files"
echo "────────────────────────────────────────────────────────────────"
ls -lh /volume1/pxe/tftp/
echo ""

echo "[7] HTTP Files"
echo "────────────────────────────────────────────────────────────────"
ls -lh /volume1/pxe/http/autoinstall/
echo ""

echo "[8] Test TFTP Locally"
echo "────────────────────────────────────────────────────────────────"
timeout 3 tftp localhost <<EOF 2>&1 | head -5
binary
get ldlinux.e64 /tmp/test_bootloader
quit
EOF
if [ -f /tmp/test_bootloader ]; then
    echo "✓ TFTP test successful!"
    rm /tmp/test_bootloader
else
    echo "✗ TFTP test failed"
fi
echo ""

echo "[9] Test HTTP"
echo "────────────────────────────────────────────────────────────────"
curl -s http://192.168.0.222/autoinstall/user-data | head -3 || echo "HTTP test failed"
echo ""

echo "[10] Router DHCP Conflict Check"
echo "────────────────────────────────────────────────────────────────"
echo "If your router also provides DHCP, it might conflict."
echo "Check your router settings and either:"
echo "  1. Disable router DHCP, OR"
echo "  2. Configure dnsmasq to only serve PXE clients"
echo ""

echo "[11] Recent dnsmasq Activity"
echo "────────────────────────────────────────────────────────────────"
journalctl -u dnsmasq -n 20 --no-pager | tail -10
echo ""

echo "════════════════════════════════════════════════════════════════"
echo "  Next Steps:"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo "1. Boot MINISFORUM and enter UEFI Shell"
echo "2. Run: ifconfig -l"
echo "3. Run: ifconfig <interface> dhcp"
echo "4. Run: ping 192.168.0.222"
echo "5. Run: tftp 192.168.0.222"
echo "6. Watch dnsmasq logs: journalctl -u dnsmasq -f"
echo ""

