# Fixing "Media Disconnected" in UEFI Shell

## The Problem
"Media disconnected" means the network interface cannot detect a physical network cable connection.

## Troubleshooting Steps

### 1. Check All Network Interfaces
```bash
ifconfig -l
```
Look for other interfaces like `eth1`, `snpo`, `enp*`, etc.

### 2. Try Other Interfaces
If `eth0` is disconnected, try:
```bash
ifconfig -l eth1
ifconfig -l snpo
```

### 3. Physical Cable Check
**CRITICAL:** Make sure the network cable is:
- ✅ Physically connected to the MINISFORUM
- ✅ Connected to your router/switch (same network as NAS: 192.168.0.x)
- ✅ Cable is not damaged
- ✅ LED lights on the network port are blinking (if visible)

### 4. Try to Reset/Reinitialize Interface
```bash
# Reset the interface
ifconfig -s eth0 static 0.0.0.0 0.0.0.0 0.0.0.0
ifconfig -s eth0 dhcp
```

### 5. Check if Interface is Enabled in BIOS
- Enter BIOS/UEFI Setup
- Look for "Network Boot" or "PXE Boot" settings
- Make sure network interface is **enabled**
- Some BIOS have "Network Stack" that needs to be enabled

### 6. Try Static IP (to test if interface works at all)
```bash
ifconfig -s eth0 static 192.168.0.50 255.255.255.0 192.168.0.1
ping 192.168.0.222
```

If static IP works but DHCP doesn't, it's a DHCP issue, not a cable issue.

### 7. Check Network Port Status
In UEFI Shell, some systems show port status. Try:
```bash
devices
```
Look for network devices and their status.

## Alternative: Boot from USB with Network

If UEFI network doesn't work, you can:
1. Create a USB boot drive with Ubuntu
2. Boot from USB
3. Once Ubuntu is running, it should have network access
4. Then you can install from network or continue setup

## Most Likely Causes

1. **Network cable not connected** (90% of cases)
2. **Wrong network interface** (eth0 might not be the active one)
3. **Network interface disabled in BIOS**
4. **Hardware issue** (rare, but possible)

## Quick Test

Try this sequence:
```bash
# 1. List all interfaces
ifconfig -l

# 2. Try each interface
ifconfig -l eth0
ifconfig -l eth1
ifconfig -l snpo

# 3. For the one that shows "Media connected", try DHCP
ifconfig -s <interface> dhcp

# 4. Check if it got an IP
ifconfig -l <interface>

# 5. Test ping
ping 192.168.0.222
```

