// Package scripts holds the TUI's [C] Custom Scripts — higher-level
// multi-step automations built on top of the api package (BIOS key spam,
// ISO boot, Ubuntu Server setup, text input, video stream launch).
//
// Script is the public registry entry; Default is the built-in list the
// TUI surfaces by default.
package scripts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"pikvm/internal/api"
	"pikvm/internal/config"
)

// Script is one entry in the [C] Custom Scripts column.
type Script struct {
	Name string
	Desc string
	Run  func(port int) string
}

// Default is the built-in set of custom scripts registered at startup.
// BootFromISOPlaceholder and BootToBIOSPlaceholder are empty-returning
// stubs — the TUI detects them by Name and opens modal pickers instead.
var Default = []Script{
	{"Boot from ISO", "Select and boot from any available ISO", BootFromISOPlaceholder},
	{"Setup Ubuntu Server", "Install SSH + Tailscale automation", SetupUbuntuServer},
	{"Boot to BIOS", "Pick a BIOS key (F7/Del/F2/F12/Esc/...) and spam it", BootToBIOSPlaceholder},
	{"Type from text.txt", "Type contents of text.txt file", TypeFromTextFile},
	{"View video stream", "Open PiKVM KVM in browser (video + keyboard/mouse)", ViewVideoStream},
	{"View video (mpv)", "Watch HDMI stream in mpv (needs websocat + mpv)", ViewVideoMpv},
}

// ----------------------------------------------------------------------------
// Placeholders — the TUI routes these to modal pickers.
// ----------------------------------------------------------------------------

func BootFromISOPlaceholder(_ int) string { return "" }
func BootToBIOSPlaceholder(_ int) string  { return "" }

// ----------------------------------------------------------------------------
// Boot to BIOS (roadmap idea #6)
// ----------------------------------------------------------------------------

// BIOSKeyOption is one entry in the BIOS-key picker.
type BIOSKeyOption struct {
	Label string // short UI label
	Key   string // passed to api.SendKey ("F7", "Delete", ...)
}

// BIOSKeyOptions is the order-of-presentation list for the BIOS picker.
var BIOSKeyOptions = []BIOSKeyOption{
	{"F7", "F7"},
	{"Del", "Delete"},
	{"F2", "F2"},
	{"F3", "F3"},
	{"F12", "F12"},
	{"Esc", "Escape"},
	{"F8", "F8"},
	{"F11", "F11"},
	{"F1", "F1"},
}

// BIOSKeyHint returns a short vendor cheat-sheet for the picker. Empty for
// keys without a well-known mapping.
func BIOSKeyHint(label string) string {
	switch label {
	case "F7":
		return "(Dell, HP boot menu)"
	case "Del":
		return "(ASUS, MSI BIOS setup)"
	case "F2":
		return "(Lenovo, Acer BIOS setup)"
	case "F12":
		return "(Dell boot menu, generic)"
	case "Esc":
		return "(HP boot menu, some Lenovo)"
	case "F11":
		return "(Asrock, some Gigabyte)"
	case "F8":
		return "(older boot menu)"
	}
	return ""
}

// BootToBIOSWithKey power-cycles port and spams opt for ~30s. Key spam
// starts immediately in a goroutine so it catches the first boot frame.
func BootToBIOSWithKey(port int, opt BIOSKeyOption) string {
	go func() {
		for i := 0; i < 60; i++ {
			_ = api.SendKey(opt.Key)
			time.Sleep(500 * time.Millisecond)
		}
	}()
	act := api.Action{Name: "Power ON", APICmd: "/switch/atx/power?port=%d&action=on", Method: "POST"}
	api.ExecuteAction(act, port)
	return fmt.Sprintf("\uf00c %s spam started! (60 presses over 30s) + Powered on port %d", opt.Label, port+1)
}

// ----------------------------------------------------------------------------
// Setup Ubuntu Server
// ----------------------------------------------------------------------------

func typeSudoCommand(command, password string) {
	_ = api.SendText(command + "\n")
	time.Sleep(2 * time.Second)
	_ = api.SendText(password + "\n")
	time.Sleep(2 * time.Second)
}

// SetupUbuntuServer automates: apt update, install openssh-server, enable
// ssh, install tailscale, authenticate tailscale, show status.
func SetupUbuntuServer(_ int) string {
	if config.TailscaleAuthKey == "" {
		return "\uf057 Error: TAILSCALE_AUTH_KEY not set in config"
	}
	if config.UbuntuPassword == "" {
		return "\uf057 Error: UBUNTU_PASSWORD not set in config"
	}

	go func() {
		time.Sleep(1 * time.Second)
		typeSudoCommand("sudo apt update", config.UbuntuPassword)
		typeSudoCommand("sudo apt install -y openssh-server", config.UbuntuPassword)
		typeSudoCommand("sudo systemctl enable ssh", config.UbuntuPassword)
		typeSudoCommand("sudo systemctl start ssh", config.UbuntuPassword)
		_ = api.SendText("curl -fsSL https://tailscale.com/install.sh | sh\n")
		time.Sleep(5 * time.Second)
		_ = api.SendText(config.UbuntuPassword + "\n")
		time.Sleep(8 * time.Second)
		typeSudoCommand(fmt.Sprintf("sudo tailscale up --authkey=%s --ssh", config.TailscaleAuthKey), config.UbuntuPassword)
		time.Sleep(2 * time.Second)
		_ = api.SendText("tailscale status\n")
		time.Sleep(3 * time.Second)
		_ = api.SendText("echo 'Setup complete! SSH with: ssh $(whoami)@$(tailscale ip -4)'\n")
	}()

	var result strings.Builder
	result.WriteString("\uf121 Ubuntu Server Setup Started!\n\n")
	result.WriteString("  Automated setup with password handling:\n\n")
	result.WriteString("  1. \uf0c8 Update package lists\n")
	result.WriteString("  2. \uf233 Install OpenSSH Server\n")
	result.WriteString("  3. \uf085 Enable & start SSH\n")
	result.WriteString("  4. \uf0c1 Install Tailscale\n")
	result.WriteString("  5. \uf023 Authenticate Tailscale\n")
	result.WriteString("  6. \uf00c Show Tailscale status\n\n")
	result.WriteString("\uf06a Watch your screen - commands + passwords typed automatically!\n")
	result.WriteString("  Each sudo command will auto-enter password after 2s delay\n\n")
	result.WriteString("  After completion, SSH to server with:\n")
	result.WriteString("  ssh username@100.xxx.xxx.xxx\n")
	return result.String()
}

// ----------------------------------------------------------------------------
// Type from text.txt
// ----------------------------------------------------------------------------

// TypeFromTextFile reads text.txt from next to the binary (or cwd) and types
// it through PiKVM's HID API.
func TypeFromTextFile(_ int) string {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Sprintf("\uf057 Error finding executable path: %v", err)
	}
	textPath := filepath.Join(filepath.Dir(execPath), "text.txt")
	if _, err := os.Stat(textPath); os.IsNotExist(err) {
		textPath = "text.txt"
	}
	textBytes, err := os.ReadFile(textPath)
	if err != nil {
		return fmt.Sprintf("\uf057 Error reading text.txt: %v\nMake sure text.txt exists in the same directory as the binary", err)
	}
	textToType := string(textBytes)
	if len(textToType) == 0 {
		return "\uf06a text.txt is empty! Add some text to type."
	}
	go func() { _ = api.SendText(textToType) }()
	return fmt.Sprintf("\uf00c Typing text from text.txt (%d characters)...\n  Content: %s\n  Check your connected device!", len(textToType), textToType)
}

// ----------------------------------------------------------------------------
// View video stream (browser)
// ----------------------------------------------------------------------------

// ViewVideoStream opens the PiKVM KVM page in the default browser.
func ViewVideoStream(port int) string {
	_ = api.SetSwitchPort(port) // non-fatal
	kvmURL := fmt.Sprintf("https://%s/", config.Host)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", kvmURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", kvmURL)
	default:
		cmd = exec.Command("xdg-open", kvmURL)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("\uf057 Could not open browser: %v\n  Open manually: %s", err, kvmURL)
	}
	return fmt.Sprintf("\uf00c Opening PiKVM in browser (video set to port %d).\n  Log in if prompted, then watch the stream.", port+1)
}

// ----------------------------------------------------------------------------
// View video (mpv)
// ----------------------------------------------------------------------------

// ViewVideoMpv opens the HDMI stream in ffplay/mpv.
func ViewVideoMpv(port int) string {
	if _, err := exec.LookPath("websocat"); err != nil {
		return "\uf057 websocat not found in PATH.\n  Install: brew install websocat\n  Run this program from a terminal where PATH includes Homebrew."
	}
	if _, err := exec.LookPath("ffplay"); err != nil {
		if _, err := exec.LookPath("mpv"); err != nil {
			return "\uf057 Need ffplay or mpv in PATH.\n  Install: brew install ffmpeg  (for ffplay) or brew install mpv"
		}
	}
	_ = api.SetSwitchPort(port)

	execPath, _ := os.Executable()
	scriptDir := filepath.Dir(execPath)
	if _, err := os.Stat(filepath.Join(scriptDir, "pikvm.sh")); os.IsNotExist(err) {
		scriptDir = "."
	}
	scriptPath := filepath.Join(scriptDir, "pikvm.sh")
	absScript, _ := filepath.Abs(scriptPath)

	if runtime.GOOS == "darwin" {
		runInTerminal := fmt.Sprintf("cd %s && ./pikvm.sh --stream", quotedPath(filepath.Dir(absScript)))
		cmd := exec.Command("osascript", "-e", "tell application \"Terminal\" to do script "+quotedAppleScript(runInTerminal))
		if err := cmd.Run(); err != nil {
			return fmt.Sprintf("\uf057 Could not open Terminal: %v\n  Run in terminal instead: cd %s && ./pikvm.sh --stream", err, scriptDir)
		}
		return fmt.Sprintf("\uf00c Opened stream in a new Terminal window (port %d). Close the window when done.", port+1)
	}

	useFfplay := false
	if _, err := exec.LookPath("ffplay"); err == nil {
		useFfplay = true
	}
	keeperURL := fmt.Sprintf("wss://%s/api/ws?stream=1", config.Host)
	keeper := exec.Command("websocat", "-k", keeperURL,
		"-H", "X-KVMD-User: "+config.User,
		"-H", "X-KVMD-Passwd: "+config.Pass,
	)
	if err := keeper.Start(); err != nil {
		return fmt.Sprintf("\uf057 Could not start stream keeper: %v", err)
	}
	defer keeper.Process.Kill()
	time.Sleep(2 * time.Second)

	mediaURL := fmt.Sprintf("wss://%s/api/media/ws?video=h264", config.Host)
	var script string
	if useFfplay {
		script = `websocat -b -B10000000 -k "$WS_MEDIA_URL" -H "X-KVMD-User: $WS_USER" -H "X-KVMD-Passwd: $WS_PASS" | ffplay -f h264 -framerate 30 -probesize 10M -analyzeduration 5M -fflags nobuffer -flags low_delay -i pipe:0 -window_title PiKVM`
	} else {
		script = `websocat -b -B10000000 -k "$WS_MEDIA_URL" -H "X-KVMD-User: $WS_USER" -H "X-KVMD-Passwd: $WS_PASS" | mpv --no-cache --demuxer-lavf-format=h264 --demuxer-lavf-o=probesize=10000000,analyzeduration=5000000 -`
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", script)
	} else {
		cmd = exec.Command("sh", "-c", script)
	}
	cmd.Dir = scriptDir
	cmd.Env = append(os.Environ(), "WS_MEDIA_URL="+mediaURL, "WS_USER="+config.User, "WS_PASS="+config.Pass)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Sprintf("\uf00c Stream ended. %v", err)
	}
	return "\uf00c Stream ended."
}

// ----------------------------------------------------------------------------
// Boot from ISO
// ----------------------------------------------------------------------------

// BootFromISOEntry uploads entry if it's local, then mounts + F7-spams.
func BootFromISOEntry(port int, entry api.IsoEntry) string {
	if entry.LocalPath != "" {
		execPath, _ := os.Executable()
		scriptDir := filepath.Dir(execPath)
		if _, err := os.Stat(filepath.Join(scriptDir, "pikvm.sh")); os.IsNotExist(err) {
			scriptDir = "."
		}
		absDir, _ := filepath.Abs(scriptDir)
		uploadCmd := fmt.Sprintf("cd %s && ./pikvm.sh --iso --upload %s", quotedBash(absDir), quotedBash(entry.LocalPath))

		if runtime.GOOS == "darwin" {
			cmd := exec.Command("osascript", "-e", "tell application \"Terminal\" to do script "+quotedAppleScript(uploadCmd))
			if err := cmd.Run(); err != nil {
				return fmt.Sprintf("\uf057 Could not open Terminal: %v\n  Run in another terminal: ./pikvm.sh --iso --upload %s", err, entry.LocalPath)
			}
			return fmt.Sprintf("\uf00c Upload started in new Terminal window.\n  When it finishes, run Boot from ISO (1) again and select %s to boot.", entry.Name)
		}
		if runtime.GOOS == "linux" {
			for _, termCmd := range []string{"gnome-terminal --", "xterm -e"} {
				var cmd *exec.Cmd
				if strings.HasPrefix(termCmd, "gnome-terminal") {
					cmd = exec.Command("gnome-terminal", "--", "bash", "-c", uploadCmd+"; echo; read -p 'Press Enter to close...'")
				} else {
					cmd = exec.Command("xterm", "-e", "bash", "-c", uploadCmd+"; echo; read -p 'Press Enter to close...'")
				}
				if err := cmd.Start(); err != nil {
					continue
				}
				return fmt.Sprintf("\uf00c Upload started in new window.\n  When it finishes, run Boot from ISO (1) again and select %s to boot.", entry.Name)
			}
		}
		cmd := exec.Command("sh", "-c", uploadCmd)
		cmd.Dir = scriptDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Sprintf("\uf057 Upload failed: %v\n  Run manually: ./pikvm.sh --iso --upload %s", err, entry.LocalPath)
		}
	}
	return BootFromSpecificISO(port, entry.Name)
}

// BootFromSpecificISO selects an already-uploaded ISO, mounts the virtual
// drive, powers the port on, and spams F7 for 30s.
func BootFromSpecificISO(port int, isoName string) string {
	var result strings.Builder
	result.WriteString(fmt.Sprintf("\uf121 Starting boot from: %s\n\n", isoName))

	result.WriteString("  \uf0a0 Step 1: Selecting ISO on PiKVM...\n")
	result.WriteString(fmt.Sprintf("    → API: POST /api/msd/set_params?image=%s\n", isoName))
	if err := api.SelectMSDImage(isoName); err != nil {
		result.WriteString(fmt.Sprintf("  \uf057 Failed to select ISO: %v\n", err))
		return result.String()
	}
	result.WriteString("  \uf00c ISO selected in PiKVM!\n\n")

	result.WriteString("  \uf0c1 Step 2: Connecting drive to server (mounting)...\n")
	result.WriteString("    → API: POST /api/msd/set_connected?connected=1\n")
	if err := api.ConnectMSD(true); err != nil {
		result.WriteString(fmt.Sprintf("  \uf057 Failed to connect drive: %v\n", err))
		return result.String()
	}
	result.WriteString("  \uf00c Virtual USB drive connected to server!\n\n")

	result.WriteString("  \uf252 Step 3: Waiting for drive recognition (2s)...\n")
	time.Sleep(2 * time.Second)
	result.WriteString("  \uf00c Ready!\n\n")

	result.WriteString("  \uf0e7 Step 4: Booting to BIOS...\n")
	go func() {
		for i := 0; i < 60; i++ {
			_ = api.SendKey("F7")
			time.Sleep(500 * time.Millisecond)
		}
	}()
	act := api.Action{Name: "Power ON", APICmd: "/switch/atx/power?port=%d&action=on", Method: "POST"}
	api.ExecuteAction(act, port)

	result.WriteString("  \uf00c F7 spam started! (60 presses over 30s)\n")
	result.WriteString(fmt.Sprintf("  \uf00c Powered on port %d\n\n", port+1))
	result.WriteString("\uf058 Boot sequence complete!\n")
	result.WriteString(fmt.Sprintf("\uf0a1 Select the USB drive from BIOS to boot: %s", isoName))
	return result.String()
}

// ----------------------------------------------------------------------------
// Shell-safe quoting helpers (used by ViewVideoMpv + BootFromISOEntry).
// ----------------------------------------------------------------------------

// quotedPath returns a path safe for sh -c "cd ..." (single-quote).
func quotedPath(p string) string {
	return "'" + strings.ReplaceAll(p, "'", "'\"'\"'") + "'"
}

// quotedBash returns a string safe for sh -c (single-quoted).
func quotedBash(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// quotedAppleScript returns a string safe for AppleScript (backslash-escape).
func quotedAppleScript(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
