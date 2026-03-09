# PiKVM Control - Go + Bubble Tea Edition

Control your PiKVM ATX power management with a beautiful TUI built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Table of Contents

- [Requirements](#requirements)
- [Features](#features)
- [Quick Start](#quick-start)
  - [Building](#building)
  - [Usage](#usage)
- [TUI Interface](#tui-interface)
- [Configuration](#configuration)
- [Custom Scripts](#custom-scripts)
- [Optional: Nerd Fonts Setup](#optional-nerd-fonts-setup)
- [Terminal Configuration](#terminal-configuration)
- [CLI Examples](#cli-examples)
- [API Reference](#api-reference)
- [Technology](#technology)
- [Color Scheme](#color-scheme)

---

## Requirements

**No special requirements!** Works with any terminal font using Unicode symbols.

**Optional:** Install a [Nerd Font](https://www.nerdfonts.com/) for enhanced icons (like yazi & eza). See [Nerd Fonts Setup](#optional-nerd-fonts-setup) below.

---

## Features

✅ **Single Binary** - No dependencies, just run it  
✅ **Auto-Detection** - Automatically detects connected extenders and available ports  
✅ **Port Selection** - Choose from available ports (validates availability)  
✅ **Simple & Clean TUI** - Compact design with pastel green highlighting  
✅ **Visual Highlighting** - Selected option in bold pastel green (#9bf09d)  
✅ **Color-coded Messages** - Success (green), warnings (orange), errors (red)  
✅ **Compact Layout** - Action and description on one line  
✅ **CLI Mode** - Quick commands for scripting  
✅ **All Actions** - Power on/off, short/long press, reset  
✅ **Custom Scripts** - Automated sequences (e.g., boot to BIOS with F7 spam)  
✅ **Keyboard Control** - Send keys via PiKVM HID  
✅ **Real-time Refresh** - Press 'r' to re-detect ports

---

## Quick Start

### Building

```bash
./build.sh              # Build the binary
./build.sh --test       # Test .env configuration
./build.sh --help       # Show build options
```

### Usage

#### Interactive TUI (Recommended)

```bash
# Launch the interactive menu
./pikvm

# Navigate with arrow keys or k/j
# Press 0-9 to select port
# Press Enter to execute action
# Press q or ESC to quit
```

#### Command Line Mode

```bash
# Show help
./pikvm help

# Turn power ON (port 0)
./pikvm on

# Turn power OFF (port 1)
./pikvm off 1

# Power button click - short press (port 0)
./pikvm click

# Power button long press - force shutdown (port 2)
./pikvm long 2

# Reset button
./pikvm reset

# Reset button long press
./pikvm reset-long
```

---

## TUI Interface

Compact and simple design with Unicode symbols:

```
╭──────────────────────────────╮
│ ⚡ PiKVM ATX Power Control   │
╰──────────────────────────────╯

  ▶ Extenders: 1  │  Available Ports: 0, 1, 2, 3

  ● Current Port: 0 ✓

  [1] Power ON (Turn power on)
  [2] Power OFF (Turn power off)
  [3] Power Click (Power button short press)
  [4] Power Long Press (Force shutdown)
  [5] Reset Click (Reset button short press)
  [6] Reset Long Press (Reset button long press)
  [7] Disconnect Drive (Disconnect virtual USB drive)

   Custom Scripts:

  [ 1] Boot to BIOS (F7) (Power on + spam F7 for BIOS entry)
  [ 2] Boot to BIOS (Del) (Power on + spam Del for BIOS entry)
  [ 3] Boot to BIOS (F2) (Power on + spam F2 for BIOS entry)  ← Selected

  ↑/↓/k/j: Navigate  │  ⏎: Execute  │  #: Port  │  ⟳: Refresh  │  ✕: Quit
```

### Symbols Used:
- ⚡ Lightning - Power/Energy
- ▶ Triangle - Extenders indicator  
- ● Circle - Current port
- ✓ Check - Success/Available
- ⚠ Warning - Unavailable/Warning
- ✗ X - Error
- ⏎ Return - Execute
- ⟳ Reload - Refresh
- ✕ Close - Quit

**Works with any font!** No Nerd Fonts required (but they look even better with them).

The selected item is shown in **bold pastel green text (#9bf09d)** - no boxes, no arrows, just clean highlighting.

### TUI Features:
- **Compact layout** - All info on one line per action
- **Auto-detect extenders** - Shows how many extenders are connected
- **Port validation** - Only allows selecting available ports
- **Visual indicators** - 🟢 for available port, ⚠️ for unavailable
- **Simple highlighting** - Selected item in pastel green (#9bf09d), no boxes or arrows
- **Clean design** - Minimal and easy to read
- **Live refresh** - Press 'r' to re-scan for ports
- **Color feedback** - Success (green), warnings (orange), errors (red)
- **Custom Scripts** - Automated multi-step sequences with keyboard input

---

## Configuration

**All scripts now read from `.env` file!** ✅

Configuration is stored in `.env` file:

```bash
PIKVM_HOST=100.65.29.115     # PiKVM IP (via Tailscale)
PIKVM_USER=admin              # Username
PIKVM_PASS=your_password      # Password
ISO_PATH=/path/to/iso         # Default ISO path
```

**Scripts that use `.env`:**
- ✅ `pikvm` (Go binary) - Main TUI & CLI commands
- ✅ `pikvm_control.py` (Python) - Alternative TUI
- ✅ `iso.sh` (Bash) - ISO manager
- ✅ `build.sh` (Bash) - Build & test

**Test configuration:**
```bash
./build.sh --test    # Verify all scripts load .env correctly
```

**Other settings:**
- **Ports:** Auto-detected (4 ports per extender)
- **Default Port:** 0 (change with 0-9 keys in TUI)
- **API:** HTTPS (self-signed cert)

**Security:**
- `.env` is gitignored to protect credentials
- Use `.env.example` as template for new setups
- Change credentials by editing `.env` file only

### How Port Detection Works

The tool automatically scans ports 0-15 on startup to detect which ports are available. Each PiKVM extender typically provides 4 ports, so:
- **1 extender** = 4 ports (0-3)
- **2 extenders** = 8 ports (0-7)
- **3 extenders** = 12 ports (0-11)
- etc.

---

## Custom Scripts

Custom scripts allow you to automate complex sequences of actions and keyboard inputs.

### Built-in Scripts

The tool comes with several pre-built automation scripts:

#### ISO Boot Scripts

- **Boot from ISO** - **Interactive ISO selector** ⭐
  1. Press Enter on "Boot from ISO" option
  2. **Interactive menu appears** with all available ISOs
  3. Navigate with **↑/↓** or **k/j** (vim-style)
  4. Press **Enter** to select an ISO
  5. Automatically mounts the virtual USB drive
  6. Powers on and spams F7 to enter BIOS
  7. Ready for you to select USB boot from BIOS menu
  
  **Features:**
  - ✅ **Fetches ISOs dynamically** from PiKVM
  - ✅ **Full keyboard navigation** (hjkl or arrows)
  - ✅ **Visual selection** (highlighted in pastel green)
  - ✅ **Works with ANY ISO** you upload
  - ✅ **No code changes needed** - just upload and boot!

#### BIOS Boot Scripts
- **Boot to BIOS (F7)** - For Dell, HP systems
- **Boot to BIOS (Del)** - For ASUS, MSI systems
- **Boot to BIOS (F2)** - For Lenovo, Acer systems

#### Automation Scripts

- **Setup Ubuntu Server** - Complete Ubuntu setup automation ⭐
  - Installs OpenSSH Server
  - Enables and starts SSH service
  - Installs Tailscale
  - Authenticates with your Tailscale auth key (from `.env`)
  - Enables Tailscale SSH
  - Shows Tailscale status
  - **Fully automated** - just watch it type!

- **Type from text.txt** - Types contents of `text.txt` file
  - Reads from `/Users/wgm0/px/pikvm/text.txt`
  - Types the entire contents via PiKVM HID
  - Perfect for passwords, commands, or any text automation
  - Edit `text.txt` to change what gets typed

Each script:
- **Immediately** starts spamming the BIOS key (60 times, 2/sec)
- Sends power on command **in parallel** (no waiting!)
- Runs for 30 seconds total
- Shows progress in real-time
- Catches boot screen from the very start

### How It Works

The scripts use PiKVM's HID (keyboard) API to send keystrokes:
```
1. Click option → IMMEDIATELY start F7 spam (background)
2. Power ON → Sent in parallel (no delay!)
3. Spam runs → 60 times at 500ms intervals (2/sec)
4. Total time → 30 seconds of continuous key presses
```

**Key advantage:** F7 spam starts BEFORE the machine even powers on, ensuring you catch the boot screen from frame 1!

### Uploading ISO Images to PiKVM

#### 🌐 **Web Interface (Recommended for Large Files >1GB)**

The **easiest and most reliable** method:

1. **Open PiKVM:** https://100.65.29.115/kvm/
2. **Login** with your credentials
3. **Navigate:** Click menu icon (≡) → **System** → **Mass Storage Device**
4. **Upload:** Click **"Upload"** or **"+"** button
5. **Select:** Choose your ISO file (`ubuntu-25.10-live-server-amd64.iso`)
6. **Wait:** Progress bar shows upload status (~5-10 minutes for 2GB)
7. **Done!** ISO is ready to use

**Why use web interface for large files?**
- ✅ No memory limitations
- ✅ Visual progress bar
- ✅ Pause/resume support
- ✅ Most reliable method
- ✅ Works for any file size

---

#### 💻 **CLI Upload (Works for files <1GB)**

```bash
# List ISOs on PiKVM
./iso.sh --list

# Test the API (small files only)
./iso.sh --test

# Upload small ISO (<1GB)
./iso.sh --upload /path/to/small.iso

# For large files: Script will warn and recommend web interface
./iso.sh --upload /path/to/large.iso
```

#### 📋 **Available Commands**

| Command | Description | Works For |
|---------|-------------|-----------|
| `./iso.sh --help` | Show all options | All |
| `./iso.sh --list` | List ISOs + storage info | All |
| `./iso.sh --storage` | Show storage space | All |
| `./iso.sh --delete <name>` | Delete an ISO | All |
| `./iso.sh --test` | Test API with 1MB file | Small files |
| `./iso.sh --upload <file>` | Upload ISO via API | Files <1GB |
| `./iso.sh --scp <file>` | Upload via SCP | All (needs SSH) |

---

#### ⚠️ **Important: File Size Limitations**

| Method | Small (<100MB) | Medium (100MB-1GB) | Large (>1GB) |
|--------|---------------|-------------------|--------------|
| **Web Interface** | ✅ Works | ✅ Works | ✅ **Best Choice** |
| **CLI API** | ✅ Works | ⚠️ May fail | ❌ Out of memory |
| **SCP** | ✅ Works | ✅ Works | ✅ Works (needs SSH) |

**For Ubuntu ISO (2.1 GB):** Use the **web interface** 👆

**Note:** SCP upload requires specific PiKVM filesystem permissions that may be restricted by default. The web interface is the most reliable method for all file sizes.

#### Using the ISO Boot Scripts

Once you have ISOs on your PiKVM:

1. **Verify ISO is uploaded:**
   ```bash
   ./iso.sh --list
   ```

2. **Launch the TUI:**
   ```bash
   ./pikvm
   ```

3. **Navigate to Custom Scripts** - You'll see:
   ```
   [1] Boot Ubuntu ISO       - Ubuntu 25.10 installer
   [2] Boot Windows 11 ISO   - Windows 11 installer
   [3] Boot to BIOS (F7)     - Generic BIOS boot
   ...
   ```

4. **Select and press Enter** - The script automatically:
   - ✅ Selects the ISO
   - ✅ Mounts virtual USB drive
   - ✅ Powers on the machine
   - ✅ Spams F7 to enter BIOS (60 times/30s)

5. **In BIOS:** Select the USB drive from boot menu

6. **Done!** Installer boots

**Tip:** Test with existing ISOs first (Windows 11 is already on your PiKVM)!

### Adding Your Own Scripts

Edit `pikvm.go` and add your script to the `customScripts` array:

```go
var customScripts = []customScript{
    {"Boot to BIOS (F7)", "Power on + spam F7 for BIOS entry", bootToBIOS},
    {"Your Script Name", "Description here", yourScriptFunction},
}
```

### Script Template

```go
func yourScriptFunction(port int) string {
    var result strings.Builder
    result.WriteString(fmt.Sprintf("\uf021 Starting your script on port %d...\n", port))
    
    // Step 1: Power action
    result.WriteString("  \uf0e7 Powering on...\n")
    act := action{"Power ON", "", "/switch/atx/power?port=%d&action=on", "POST"}
    executeAction(act, port)
    
    // Step 2: Wait
    result.WriteString("  \uf252 Waiting 3 seconds...\n")
    time.Sleep(3 * time.Second)
    
    // Step 3: Send keyboard keys (example: spam Esc 10 times)
    result.WriteString("  \uf11c Pressing Esc repeatedly...\n")
    for i := 0; i < 10; i++ {
        sendKey("Escape")
        time.Sleep(500 * time.Millisecond) // 2 presses per second
    }
    
    result.WriteString("\n\uf00c Script complete!")
    return result.String()
}
```

### Available Keys

The `sendKey()` function accepts standard key names:

#### Function Keys
```go
sendKey("F1")   // F1-F12
sendKey("F7")
sendKey("F12")
```

#### Special Keys
```go
sendKey("Escape")
sendKey("Enter")
sendKey("Delete")
sendKey("Backspace")
sendKey("Tab")
sendKey("Space")
```

#### Modifier Keys
```go
sendKey("Shift")
sendKey("Control")
sendKey("Alt")
```

#### Arrow Keys
```go
sendKey("ArrowUp")
sendKey("ArrowDown")
sendKey("ArrowLeft")
sendKey("ArrowRight")
```

#### Letter/Number Keys
```go
sendKey("a")    // Lowercase letters
sendKey("A")    // Uppercase letters
sendKey("1")    // Numbers
```

### Example Custom Scripts

#### Boot to USB
```go
func bootToUSB(port int) string {
    var result strings.Builder
    result.WriteString("\uf021 Booting to USB drive...\n")
    
    // Power on
    act := action{"Power ON", "", "/switch/atx/power?port=%d&action=on", "POST"}
    executeAction(act, port)
    
    time.Sleep(2 * time.Second)
    
    // Press F12 for boot menu (common on many systems)
    result.WriteString("  \uf11c Opening boot menu (F12)...\n")
    for i := 0; i < 10; i++ {
        sendKey("F12")
        time.Sleep(300 * time.Millisecond)
    }
    
    result.WriteString("\n\uf00c Ready - select USB from boot menu")
    return result.String()
}
```

#### Windows Safe Mode Boot
```go
func bootSafeMode(port int) string {
    var result strings.Builder
    result.WriteString("\uf021 Booting to Safe Mode...\n")
    
    act := action{"Power ON", "", "/switch/atx/power?port=%d&action=on", "POST"}
    executeAction(act, port)
    
    time.Sleep(2 * time.Second)
    
    // Spam F8 for Advanced Boot Options
    result.WriteString("  \uf11c Pressing F8 for Advanced Boot...\n")
    for i := 0; i < 15; i++ {
        sendKey("F8")
        time.Sleep(300 * time.Millisecond)
    }
    
    result.WriteString("\n\uf00c Select Safe Mode from menu")
    return result.String()
}
```

#### Network Boot (PXE)
```go
func bootPXE(port int) string {
    var result strings.Builder
    result.WriteString("\uf021 Starting PXE network boot...\n")
    
    act := action{"Power ON", "", "/switch/atx/power?port=%d&action=on", "POST"}
    executeAction(act, port)
    
    time.Sleep(2 * time.Second)
    
    // F12 is common for PXE boot
    result.WriteString("  \uf11c Triggering network boot (F12)...\n")
    for i := 0; i < 12; i++ {
        sendKey("F12")
        time.Sleep(400 * time.Millisecond)
    }
    
    result.WriteString("\n\uf00c PXE boot initiated")
    return result.String()
}
```

### Rebuild After Adding Scripts

```bash
./build.sh
# or
go build -o pikvm pikvm.go
```

---

## Optional: Nerd Fonts Setup

The PiKVM tool works with standard Unicode symbols, but for the best experience (like yazi and eza), you can install a Nerd Font.

### Quick Install

#### Option 1: Homebrew (Easiest)

```bash
# Install a popular Nerd Font
brew install --cask font-hack-nerd-font

# Or install JetBrains Mono
brew install --cask font-jetbrains-mono-nerd-font

# Or install FiraCode
brew install --cask font-fira-code-nerd-font
```

#### Option 2: Manual Download

1. Visit https://www.nerdfonts.com/
2. Download your preferred font (Hack, JetBrains Mono, FiraCode recommended)
3. Unzip and double-click the `.ttf` files to install
4. Select "Install Font"

### Icon Reference

The app uses Font Awesome icons from Nerd Fonts:
- `\uf0e7` - fa-bolt (lightning)
- `\uf519` - fa-server-waveform (network)
- `\uf0e4` - fa-sitemap (port/network)
- `\uf058` - fa-check-circle
- `\uf06a` - fa-exclamation-circle
- `\uf057` - fa-times-circle
- `\uf04b` - fa-play
- `\uf292` - fa-hashtag
- `\uf021` - fa-refresh
- `\uf011` - fa-power-off
- `\uf00c` - fa-check
- `\uf071` - fa-exclamation-triangle

---

## Terminal Configuration

After installing a Nerd Font, configure your terminal to use it.

### Kitty (Recommended)
Edit `~/.config/kitty/kitty.conf`:
```bash
font_family      JetBrainsMono Nerd Font Mono
bold_font        auto
italic_font      auto
bold_italic_font auto
```

Then reload: `Cmd+Shift+F5` or restart Kitty

### iTerm2
1. Open iTerm2 → Preferences (Cmd+,)
2. Profiles → Text → Font
3. Click "Change Font"
4. Select "JetBrainsMono Nerd Font Mono"
5. Size: 13-14 recommended
6. Click OK

### Terminal.app
1. Terminal → Preferences
2. Profiles → Font → Change
3. Select "JetBrainsMono Nerd Font Mono"
4. Size: 13-14
5. Set as Default profile

### Alacritty
Edit `~/.config/alacritty/alacritty.yml`:
```yaml
font:
  normal:
    family: "JetBrainsMono Nerd Font Mono"
  size: 13.0
```

### WezTerm
Edit `~/.wezterm.lua`:
```lua
config.font = wezterm.font('JetBrainsMono Nerd Font Mono')
config.font_size = 13.0
```

### Test Icons

After configuring, run:
```bash
./pikvm
```

You should now see beautiful icons:
-  Lightning bolt (header)
-  Server/network (extenders)
-  Port indicator
-  Check mark (success)
-  Warning triangle
-  Play (execute)
-  Refresh
-  Power (quit)

---

## CLI Examples

### Graceful Shutdown
```bash
./pikvm click 0
```

### Force Shutdown
```bash
./pikvm long 0
```

### Power Cycle Multiple Ports
```bash
./pikvm off 1 && sleep 5 && ./pikvm on 1
```

---

## API Reference

The tool uses PiKVM's ATX and HID API endpoints:

### ATX Power Control
- `POST /api/switch/atx/power?port={N}&action={on|off}`
- `POST /api/switch/atx/click?port={N}&button={power|power_long|reset|reset_long}`

### HID Keyboard Control
- Endpoint: `/api/hid/events/send_key`
- Method: `POST`
- Payload: `{"key": "KeyName", "state": true}`

For more details, see PiKVM documentation: https://docs.pikvm.org/

---

## Technology

Built with the amazing Charm stack:
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - Powerful TUI framework 🏗
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - Style, format and layout for terminals 💄

**Optional:** [Nerd Fonts](https://www.nerdfonts.com/) for enhanced icons

---

## Color Scheme

### Pastel Green Theme

- **Pastel Green** (#9bf09d / RGB 155,240,157) - Selected items, headers, port info, and success messages
- **Gold** (#FFD700) - Warnings
- **Light Red** (#FF5F5F) - Errors
- **Light Gray** (#BBBBBB) - Unselected items
- **Gray** (#888888) - Help text
