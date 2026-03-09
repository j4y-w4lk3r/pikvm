# UEFI Shell PXE Debugging Guide

## Entering UEFI Shell

1. Boot MINISFORUM
2. Press the key to enter boot menu (usually F7, F12, or Del)
3. Select "UEFI: Built-in EFI Shell" or "Enter Setup" → Shell

## Network Debugging Commands

### 1. List Network Interfaces
```bash
ifconfig -l
```
Shows all available network interfaces (usually `eth0`, `eth1`, `snpo`, etc.)

### 2. Check Interface Status
```bash
ifconfig -s <interface>
```
Example: `ifconfig -s eth0`

### 3. Get IP via DHCP (UEFI Shell syntax)
```bash
ifconfig -s eth0 dhcp
```
**Important:** UEFI Shell uses `-s` flag before interface name!

Alternative syntax if above doesn't work:
```bash
ifconfig eth0 -s dhcp
```

This should:
- Request IP from DHCP server
- Show assigned IP address
- Show gateway, DNS, etc.

### 4. Set Static IP (if DHCP fails)
```bash
ifconfig -s eth0 static 192.168.0.50 255.255.255.0 192.168.0.1
```
**UEFI Shell syntax:** `ifconfig -s <interface> static <ip> <netmask> <gateway>`

Example: `ifconfig -s eth0 static 192.168.0.50 255.255.255.0 192.168.0.1`

### 5. Test Network Connectivity
```bash
ping 192.168.0.222
```
Test if you can reach the NAS (PXE server)

### 6. Test TFTP Connection
**Note:** UEFI Shell doesn't have a `tftp` command. You need to use PXE boot directly.

Instead, try:
```bash
pxeboot
```

Or manually chainload:
```bash
dhcp
```
Then use the network boot option from the boot menu.

### 7. Check PXE Configuration
```bash
pxe
```
Shows PXE configuration

### 8. Manual PXE Boot
```bash
pxeboot
```

## Complete Debugging Sequence (UEFI Shell)

```bash
# 1. List interfaces
ifconfig -l

# 2. Check interface status (if Media disconnected, check cable!)
ifconfig -l eth0

# 3. Get DHCP (UEFI Shell syntax - use -s flag!)
ifconfig -s eth0 dhcp

# 4. Check if you got an IP
ifconfig -l eth0

# 5. Ping the PXE server
ping 192.168.0.222

# 6. If ping works, try PXE boot
pxeboot
```

## ⚠️ IMPORTANT: "Media disconnected" Issue

If you see **"Media disconnected"**, this means:
1. **Network cable not connected** - Check physical cable connection
2. **Interface not active** - Try: `ifconfig -s eth0 dhcp` to activate
3. **Wrong interface** - Try other interfaces: `ifconfig -l` to see all

**Fix for Media Disconnected:**
```bash
# Try to activate the interface
ifconfig -s eth0 dhcp

# If that doesn't work, check if cable is connected
# Then try static IP to test:
ifconfig -s eth0 static 192.168.0.50 255.255.255.0 192.168.0.1
ping 192.168.0.222
```

## Common Issues

### "No network interface found"
- Check cable connection
- Try different interface (eth1, snpo, etc.)

### "DHCP failed"
- Router DHCP might be conflicting
- Check if dnsmasq is running: `ssh root@100.120.185.99 "systemctl status dnsmasq"`
- Check firewall: `ssh root@100.120.185.99 "iptables -L -n | grep -E '67|69'"`

### "TFTP connection refused"
- Check dnsmasq TFTP: `ssh root@100.120.185.99 "netstat -tulpn | grep 69"`
- Check file exists: `ssh root@100.120.185.99 "ls -lh /volume1/pxe/tftp/ldlinux.e64"`

### "File not found" in TFTP
- Check TFTP root: `ssh root@100.120.185.99 "cat /etc/dnsmasq.d/pxe.conf | grep tftp-root"`

## Alternative: Use iPXE

If standard PXE doesn't work, you can chainload iPXE:

1. Download iPXE: `wget http://boot.ipxe.org/ipxe.efi`
2. Boot from USB with iPXE
3. In iPXE shell:
   ```
   dhcp
   chain http://192.168.0.222/pxelinux.cfg/default
   ```

