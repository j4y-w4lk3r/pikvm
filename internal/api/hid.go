package api

import (
	"fmt"
	"strings"
	"time"
)

// SendKey sends a keyboard key press to PiKVM.
//
// Special keys (F-keys, Delete, Escape, ...) go via /hid/events/send_key.
// Regular printable text goes via /hid/print.
func SendKey(key string) error {
	var endpoint string
	if IsSpecialKey(key) {
		endpoint = "/hid/events/send_key?key=" + key
	} else {
		endpoint = "/hid/print?text=" + key
	}
	resp, err := Do("POST", endpoint, nil, 0)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	SessionLog("hid_key", map[string]interface{}{"key": key})
	return nil
}

// SendText POSTs raw text to /hid/print so it gets typed on the target.
func SendText(text string) error {
	req, cancel, err := NewRequest("POST", "/hid/print", strings.NewReader(text), 30*time.Second)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := RunRequest(req, cancel)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Log length only — never log password text.
	SessionLog("hid_text", map[string]interface{}{"bytes": len(text)})
	return nil
}

// IsSpecialKey returns true for keys that should be sent via the send_key
// endpoint rather than /hid/print.
func IsSpecialKey(key string) bool {
	specialKeys := map[string]bool{
		"F1": true, "F2": true, "F3": true, "F4": true,
		"F5": true, "F6": true, "F7": true, "F8": true,
		"F9": true, "F10": true, "F11": true, "F12": true,
		"Delete": true, "Escape": true, "Enter": true,
		"Tab": true, "Backspace": true,
		"ArrowUp": true, "ArrowDown": true, "ArrowLeft": true, "ArrowRight": true,
	}
	return specialKeys[key]
}

// PixelToPiKVM converts top-left pixel coords to PiKVM absolute mouse coords (origin at screen center).
func PixelToPiKVM(x, y, width, height int) (toX, toY int) {
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}
	toX = x - width/2
	toY = y - height/2
	return toX, toY
}

// SendMouseMove moves the pointer to absolute PiKVM coordinates (0,0 = screen center).
func SendMouseMove(toX, toY int) error {
	endpoint := fmt.Sprintf("/hid/events/send_mouse_move?to_x=%d&to_y=%d", toX, toY)
	resp, err := Do("POST", endpoint, nil, 10*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	SessionLog("hid_mouse_move", map[string]interface{}{"to_x": toX, "to_y": toY})
	return nil
}

// SendMouseButton sends a mouse button press or release (left|right|middle).
func SendMouseButton(button string, down bool) error {
	state := "false"
	if down {
		state = "true"
	}
	endpoint := fmt.Sprintf("/hid/events/send_mouse_button?button=%s&state=%s", button, state)
	resp, err := Do("POST", endpoint, nil, 10*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	SessionLog("hid_mouse_button", map[string]interface{}{"button": button, "state": down})
	return nil
}

// ClickPixel moves to a pixel (top-left origin) and left-clicks.
func ClickPixel(x, y, width, height int) error {
	toX, toY := PixelToPiKVM(x, y, width, height)
	if err := SendMouseMove(toX, toY); err != nil {
		return err
	}
	time.Sleep(80 * time.Millisecond)
	if err := SendMouseButton("left", true); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	return SendMouseButton("left", false)
}
