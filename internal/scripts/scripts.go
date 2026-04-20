// Package scripts holds the TUI's [C] Custom Scripts — higher-level
// multi-step automations built on top of the api package (BIOS key spam,
// ISO boot, Ubuntu Server setup, text input, video stream launch).
//
// Script is the public registry entry; Default is the built-in list the
// TUI surfaces by default.
package scripts

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/pkg/browser"

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
	return fmt.Sprintf("\uf00c %s spam started! (60 presses over 30s) + Powered on port %s", opt.Label, api.FormatPort(port))
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

// ViewVideoStream opens the PiKVM KVM page in the default browser via the
// github.com/pkg/browser package (cross-platform — no manual switch on
// runtime.GOOS or exec.Command("open"/"xdg-open"/"rundll32")).
func ViewVideoStream(port int) string {
	_ = api.SetSwitchPort(port) // non-fatal
	kvmURL := fmt.Sprintf("https://%s/", config.Host)

	// pkg/browser writes internal diagnostic noise to stderr (e.g. "open:
	// no such file or directory" for fallbacks); route it to /dev/null so
	// the TUI's alt-screen doesn't flicker.
	browser.Stderr = os.NewFile(0, os.DevNull)

	if err := browser.OpenURL(kvmURL); err != nil {
		return fmt.Sprintf("\uf057 Could not open browser: %v\n  Open manually: %s", err, kvmURL)
	}
	return fmt.Sprintf("\uf00c Opening PiKVM in browser (video set to port %s).\n  Log in if prompted, then watch the stream.", api.FormatPort(port))
}

// ----------------------------------------------------------------------------
// View video (mpv)
// ----------------------------------------------------------------------------

// ViewVideoMpv opens the HDMI stream in ffplay (or mpv fallback). The
// pipeline used to be:
//
//	websocat … | ffplay
//
// wrapped in an osascript/Terminal popup on macOS because websocat wanted
// a real TTY. We replaced websocat with a native coder/websocket dial and
// ffplay's stdin with an io.Writer fed by the WS goroutine, so now the
// whole thing runs in-process — no subprocess popup, no bash, no
// websocat dependency. ffplay still handles the video window itself
// (SDL on macOS, X11 on Linux).
func ViewVideoMpv(port int) string {
	// Pick a player that's actually installed.
	var player string
	var playerArgs []string
	if _, err := exec.LookPath("ffplay"); err == nil {
		player = "ffplay"
		playerArgs = []string{
			"-f", "h264",
			"-framerate", "30",
			"-probesize", "10M",
			"-analyzeduration", "5M",
			"-fflags", "nobuffer",
			"-flags", "low_delay",
			"-i", "pipe:0",
			"-window_title", "PiKVM",
		}
	} else if _, err := exec.LookPath("mpv"); err == nil {
		player = "mpv"
		playerArgs = []string{
			"--no-cache",
			"--demuxer-lavf-format=h264",
			"--demuxer-lavf-o=probesize=10000000,analyzeduration=5000000",
			"-",
		}
	} else {
		return "\uf057 Need ffplay or mpv in PATH.\n  Install: brew install ffmpeg (for ffplay) or brew install mpv"
	}

	_ = api.SetSwitchPort(port) // non-fatal

	// Dial the media WebSocket natively.
	ctx, cancel := context.WithCancel(context.Background())
	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	headers := http.Header{}
	headers.Set("X-KVMD-User", config.User)
	headers.Set("X-KVMD-Passwd", config.Pass)

	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	mediaURL := fmt.Sprintf("wss://%s/api/media/ws?video=h264", config.Host)
	conn, _, err := websocket.Dial(dialCtx, mediaURL, &websocket.DialOptions{
		HTTPClient: httpClient,
		HTTPHeader: headers,
	})
	dialCancel()
	if err != nil {
		cancel()
		return fmt.Sprintf("\uf057 Could not connect to video stream: %v", err)
	}
	conn.SetReadLimit(16 * 1024 * 1024) // H.264 keyframes can be big

	// Spawn the player with stdin piped from us. Stdout/stderr go to
	// /dev/null so chatter doesn't clobber the TUI's alt-screen.
	cmd := exec.Command(player, playerArgs...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		_ = conn.Close(websocket.StatusInternalError, "stdin pipe failed")
		return fmt.Sprintf("\uf057 %s stdin pipe: %v", player, err)
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		_ = conn.Close(websocket.StatusInternalError, "player start failed")
		return fmt.Sprintf("\uf057 %s start: %v", player, err)
	}

	// Copy WS frames → player stdin in the background. Stops when either
	// the WS read errors (server closed / network drop) or stdin write
	// errors (player window closed).
	go func() {
		defer cancel()
		defer func() { _ = stdin.Close() }()
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "stream done") }()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if _, err := stdin.Write(data); err != nil {
				return
			}
		}
	}()

	// Reap the child process when the user closes its window (otherwise
	// it'd become a zombie until the TUI quits).
	go func() {
		_ = cmd.Wait()
		cancel()
		_ = conn.Close(websocket.StatusNormalClosure, "player exited")
	}()

	return fmt.Sprintf("\uf00c Video stream opened in %s (port %s). Close its window when done.", player, api.FormatPort(port))
}

// ----------------------------------------------------------------------------
// Boot from ISO
// ----------------------------------------------------------------------------

// BootFromISOEntry handles booting from an entry the user already chose.
//
// For PiKVM-side ISOs (entry.LocalPath == "") it runs the full mount + F7
// boot sequence synchronously.
//
// For LOCAL ISOs (entry.LocalPath != "") it returns ErrNeedsUpload so the
// TUI can kick off a native in-process upload with a progress bar (via the
// UploadISO function in the api package). The old subprocess-in-Terminal
// behaviour is gone.
func BootFromISOEntry(port int, entry api.IsoEntry) string {
	if entry.LocalPath != "" {
		// Caller must handle ErrNeedsUpload by calling api.UploadISO then
		// BootFromSpecificISO themselves (async, with progress in the TUI).
		// We preserve the error sentinel in the returned string so CLI
		// callers that don't understand the async flow get a clear message.
		return fmt.Sprintf("\uf071 Local ISO needs upload first — use the TUI to upload with live progress,\n  or run: ./pikvm.sh --iso --upload %s", entry.LocalPath)
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
	result.WriteString(fmt.Sprintf("  \uf00c Powered on port %s\n\n", api.FormatPort(port)))
	result.WriteString("\uf058 Boot sequence complete!\n")
	result.WriteString(fmt.Sprintf("\uf0a1 Select the USB drive from BIOS to boot: %s", isoName))
	return result.String()
}

