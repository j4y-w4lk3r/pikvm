package api

import (
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
	return nil
}

// SendText POSTs raw text to /hid/print so it gets typed on the target.
// Long text needs the body channel (not a query param).
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
