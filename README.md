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
✅ **Auto-Detection** - One `GET /api/switch` call detects extenders, ports, and current active port  
✅ **Modal Port Selection** - `e` picks extender (1-2), `p` picks port (1-4); auto-syncs PiKVM `set_active`  
✅ **Simple & Clean TUI** - Compact two-column design with pastel green highlighting  
✅ **Visual Highlighting** - Selected option in bold pastel green (#9bf09d)  
✅ **Color-coded Messages** - Success (green), warnings (orange), errors (red)  
✅ **Compact Layout** - Action and description on one line  
✅ **CLI Mode** - Quick commands for scripting  
✅ **All Actions** - Power on/off, short/long press, reset  
✅ **Custom Scripts** - Automated sequences (e.g., boot to BIOS with F7 spam)  
✅ **Keyboard Control** - Send keys via PiKVM HID  
✅ **Real-time Refresh** - Press 'r' to re-fetch switch state from PiKVM

---

## Quick Start

### Building

```bash
./pikvm.sh --build           # Build the Go binary
./pikvm.sh --build --test    # Test .env configuration
./pikvm.sh --build --help    # Show build options
./pikvm.sh --help            # All shell commands (build / iso / stream)
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

#### Shell helpers (`pikvm.sh`)

```bash
./pikvm.sh --iso --list      # ISO / MSD (same as old iso.sh)
./pikvm.sh --stream          # H.264 stream test (optional port: ./pikvm.sh --stream 2)
./pikvm.sh --build           # Compile the Go TUI + ensure `.venv` (Python deps)
./pikvm.sh --sequence        # Run default JSON sequence (see `automation/seq-script/`)
```

#### Local screen automation (`automation/pikvm.py automate`)

Put PNG template crops under `automation/images/`, focus the PiKVM browser window, then:

```bash
python3 -m venv .venv
source .venv/bin/activate          # Windows: .venv\Scripts\activate
pip install -r requirements.txt
python3 automation/pikvm.py automate --image my_button.png --text "hello world"
```

The repo includes `requirements.txt` (requests, Pillow, pyautogui, opencv-python). A local **`.venv/`** is gitignored; use it so `python3` is not the bare system interpreter without those packages.

This polls the **local** display until the template appears, optionally clicks it, then types the text. For fuzzy matching add `--confidence 0.85` (requires `opencv-python`). On macOS, grant **Accessibility** to Terminal or your IDE so PyAutoGUI can send keystrokes.

#### PiKVM API screenshots (`automation/pikvm.py capture`)

Saves JPEGs from PiKVM’s HTTP snapshot API (active **switch port**, same 0-based index as the TUI). By default uses **port 2**, **every 1s**, into `./automation/images` (no extra Python deps beyond the stdlib; needs **websocat** unless something else is already keeping the stream alive):

```bash
python3 automation/pikvm.py capture
python3 automation/pikvm.py capture --port 2 --interval 1 --output-dir /Users/wgm0/px/pikvm/automation/images
python3 automation/pikvm.py capture --duration 60 --no-keeper   # stream already open in browser
```

By default, **`switch/set_active` runs before every snapshot** so the HDMI mux stays on the requested port (helps avoid “wrong port” frames). Tune with `--settle`, `--warmup`, or `--no-reassert-port` if your PiKVM is slow or you see flicker. Use `-v` to print a snippet of `GET /api/switch` once for debugging.

#### Multi-step GUI scripts (`automation/pikvm.py run-sequence`)

Declarative JSON under `automation/seq-script/` (see `automation/seq-script/FORMAT.txt`). From the repo root you can use the shell wrapper (no manual `source .venv`):

```bash
./pikvm.sh --sequence
./pikvm.sh --sequence automation/seq-script/port2-alarm.json
./pikvm.sh --sequence automation/seq-script/port2-alarm.json --verbose
```

Or call Python directly:

```bash
.venv/bin/python automation/pikvm.py run-sequence automation/seq-script/port2-alarm.json
```

- **`wait_image_pikvm`** — OpenCV template match on **PiKVM’s HTTP snapshot** for a given **switch port** (see JSON `pikvm_port`). This is the right choice when you care about **port 2’s video**, not a macOS screenshot (PyAutoGUI’s `wait_image` needs **Screen Recording** permission and only sees your **local** display).
- **`wait_image`** — PyAutoGUI on the **Mac** screen only.
- **`type` / `write`, `key` / `press`, `sleep`** — if the sequence includes `wait_image_pikvm`, these default to **PiKVM HID** (`/api/hid/print`, `send_key`) so keys go to the guest; otherwise PyAutoGUI on this machine.

Optional flags: `--pikvm-port`, `--no-stream-keeper`, `--warmup`, `--snapshot-quality`, `-v` / `--verbose` (or `PIKVM_VERBOSE=1`) to log every `set_active`, match scores, and `GET /api/switch` JSON. API ports are **0-based**; the web UI label is often **one higher** (API `2` → UI “Port 3”).

---

## TUI Interface

Compact two-column layout. Extender and port pickers are split so a multi-extender PiKVM (e.g. 2 × 4 ports) shows clean rows of `[1] [2]` and `[1] [2] [3] [4]` instead of one big "Port 1-8" line:

```
╭──────────────────────────────╮
│ ⚡ PiKVM ATX Power Control   │
╰──────────────────────────────╯

  [E] Extender: [1] [2]
  [P] Port:     [1] [2] [3] [4]
  󰈎 Selected: 2.4   ✓

  [O] Operations:                       [C] Custom Scripts:
    [1] Power ON                          [1] Boot from ISO
    [2] Power OFF                         [2] Setup Ubuntu Server
    [3] Power Click                       [3] Boot to BIOS (F7)
    [4] Power Long Press                  [4] Boot to BIOS (Del)
    [5] Reset Click                       ...
    [6] Reset Long Press
    [7] Disconnect Drive

  e: Extender  p: Port  o: Operations  c: Scripts  1-9/Enter  ESC: back  r: Refresh  q: Quit
```

### Selecting an extender / port

The TUI is **modal**: you press a letter to focus a section, then digits to pick within it. ESC exits the section.

| Action                                  | Keys                |
| --------------------------------------- | ------------------- |
| Switch to extender 2                    | `e` then `2`        |
| Switch to port 3 on the current extender | `p` then `3`        |
| Both at once (e.g. extender 2 / port 1) | `e 2` then `p 1`    |
| Run an operation (Power ON, etc.)       | `o` then `1`-`7`    |
| Run a custom script                     | `c` then `1`-`9`    |
| Refresh switch state from PiKVM         | `r`                 |
| Cancel current section / quit           | `ESC` or `q`        |

When you change extender or port, the TUI **automatically calls** `POST /api/switch/set_active` so video/USB/HID immediately follow your selection on the PiKVM. The status line shows `✓` when your selection matches what PiKVM reports as active, or a warning if they're out of sync.

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
- **Two-column layout** - Operations on the left, custom scripts on the right
- **Split extender / port pickers** - One row per concept: `[E] Extender: [1] [2]` and `[P] Port: [1] [2] [3] [4]`
- **Fast startup** - Topology is fetched in one `GET /api/switch` call (~0.5s) instead of 16 brute-force probes
- **Auto-sync video** - Selecting an extender or port immediately calls `POST /api/switch/set_active`
- **Sync indicator** - `✓` when local selection matches PiKVM, `⚠` (with PiKVM-side port) when out of sync
- **Simple highlighting** - Focused section in pastel green (#9bf09d)
- **Live refresh** - Press `r` to re-fetch the switch state
- **Color feedback** - Success (green), warnings (orange), errors (red)
- **Custom Scripts** - Automated multi-step sequences with keyboard input

---

## Configuration

**All scripts now read from `.env` file!** ✅

Configuration is stored in `.env` file:

```bash
PIKVM_HOST=100.64.183.14     # PiKVM IP (via Tailscale)
PIKVM_USER=admin              # Username
PIKVM_PASS=your_password      # Password
ISO_PATH=/path/to/iso         # Default ISO path
```

**Scripts that use `.env`:**
- ✅ `pikvm` (Go binary) - Main TUI & CLI commands
- ✅ `automation/pikvm.py` (Python) - ISO upload helpers, local GUI automation (`automate` with PyAutoGUI), PiKVM snapshot/capture, JSON sequences
- ✅ `pikvm.sh` (Bash) - Build, ISO manager, video stream test, sequence runner (`--build`, `--iso`, `--stream`, `--sequence`)

**Test configuration:**
```bash
./pikvm.sh --build --test    # Verify .env and tools
```

**Other settings:**
- **Ports:** Auto-detected via `GET /api/switch` (returns extender count + ports per extender)
- **Default Port:** Whatever PiKVM reports as currently active on launch (change with `e` / `p` + digits in TUI)
- **API:** HTTPS (self-signed cert)

**Security:**
- `.env` is gitignored to protect credentials
- Use `.env.example` as template for new setups
- Change credentials by editing `.env` file only

### How Port Detection Works

The tool calls `GET /api/switch` once on startup and parses the canonical topology returned by PiKVM:

- `result.model.units[]` → number of extenders
- `result.model.ports[]` → every port (with `id` like `"1.3"`, `unit`, `channel`)
- `result.summary.active_port` → the currently-active linear port (used to seed the TUI)

Linear port indices map to `extender.port` like this:

| Linear | Extender | Port |
| ------ | -------- | ---- |
| 0      | 1        | 1    |
| 3      | 1        | 4    |
| 4      | 2        | 1    |
| 7      | 2        | 4    |

So `e 2` then `p 3` selects linear port `6` (`extender 2 / port 3`). All ATX endpoints (`/switch/atx/power`, `/switch/atx/click`, etc.) accept the linear index in `?port=N`.

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

1. **Open PiKVM:** https://100.64.183.14/kvm/
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
./pikvm.sh --iso --list

# Test the API (small files only)
./pikvm.sh --iso --test

# Upload small ISO (<1GB)
./pikvm.sh --iso --upload /path/to/small.iso

# For large files: Script will warn and recommend web interface
./pikvm.sh --iso --upload /path/to/large.iso
```

#### 📋 **Available Commands**

| Command | Description | Works For |
|---------|-------------|-----------|
| `./pikvm.sh --iso --help` | Show all options | All |
| `./pikvm.sh --iso --list` | List ISOs + storage info | All |
| `./pikvm.sh --iso --storage` | Show storage space | All |
| `./pikvm.sh --iso --delete <name>` | Delete an ISO | All |
| `./pikvm.sh --iso --test` | Test API with 1MB file | Small files |
| `./pikvm.sh --iso --upload <file>` | Upload ISO via API | Files <1GB |
| `./pikvm.sh --iso --scp <file>` | Upload via SCP | All (needs SSH) |

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
   ./pikvm.sh --iso --list
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
./pikvm.sh --build
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
