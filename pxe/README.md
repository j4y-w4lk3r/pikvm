# PXE Boot Setup

Network boot setup for MINISFORUM 795S7 from UGREEN NAS.

## Quick Start

```bash
# Extract kernel/initrd and generate config
./pxe.sh --extract --config

# Generate NAS setup script
./pxe.sh --setup

# Copy everything to NAS
./pxe.sh --deploy

# Or do it all at once
./pxe.sh --extract --config --setup --deploy
```

## Commands

- `--extract` - Extract kernel/initrd from Ubuntu ISO
- `--config` - Generate autoinstall config files
- `--setup` - Generate NAS setup script
- `--deploy` - Copy files to NAS
- `--test` - Test PXE configuration
- `--help` - Show help

## Configuration

Copy `.env.example` to `.env` in the project root (`.env` is gitignored):

```bash
cp .env.example .env
# edit .env — UBUNTU_PASSWORD is required for --config
```

```bash
ISO_PATH=/path/to/ubuntu-25.10-live-server-amd64.iso
UBUNTU_PASSWORD=your-password
NAS_HOST=<your-nas-host>     # e.g. nas.tailnet.ts.net or 100.64.0.20
NAS_USER=<your-nas-user>
```

## Setup Steps

1. **Extract files:**
   ```bash
   ./pxe.sh --extract --config
   ```

2. **Deploy to NAS:**
   ```bash
   ./pxe.sh --deploy
   ```

3. **SSH to NAS and run setup:**
   ```bash
   ssh "$NAS_USER@$NAS_HOST"
   bash ~/pxe/nas_setup.sh
   ```
   
   (Setup script configures dnsmasq/nginx to use `/home/lsy/pxe`)

4. **Files are automatically deployed to:**
   ```bash
   /home/lsy/pxe/tftp/vmlinuz
   /home/lsy/pxe/tftp/initrd
   /home/lsy/pxe/http/autoinstall/user-data
   /home/lsy/pxe/http/autoinstall/meta-data
   ```
   
   (No manual copying needed - `--deploy` handles it!)

5. **Start services:**
   ```bash
   sudo systemctl restart dnsmasq nginx
   sudo systemctl enable dnsmasq nginx
   ```

6. **Boot MINISFORUM with network boot enabled!**

## Files Generated

- `files/vmlinuz` - Linux kernel
- `files/initrd` - Initial ramdisk
- `files/user-data` - Autoinstall config
- `files/meta-data` - Instance metadata
- `files/nas_setup.sh` - NAS setup script

## Directory Structure on NAS

```
/home/lsy/pxe/
├── tftp/              # TFTP files (kernel, initrd, bootloader)
│   ├── vmlinuz
│   ├── initrd
│   ├── ldlinux.e64
│   └── pxelinux.cfg/
│       └── default
├── http/              # HTTP files (autoinstall config)
│   └── autoinstall/
│       ├── user-data
│       └── meta-data
└── nas_setup.sh       # Setup script
```

## Troubleshooting

- **Extraction fails:** Install `7zip` or use manual mount
- **Cannot connect to NAS:** Check SSH access
- **PXE boot doesn't work:** Check dnsmasq logs: `sudo journalctl -u dnsmasq`
- **Permission errors:** Make sure `/home/lsy/pxe` is owned by `lsy:lsy` and has 755 permissions

