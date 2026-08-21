package cli

import (
	"fmt"
	"strconv"
	"strings"

	"pikvm/internal/api"
)

func cliMouse(jsonMode bool, args []string) {
	if len(args) == 0 {
		die(jsonMode, "mouse", fmt.Errorf("usage: pikvm mouse click <x> <y> [--screen WxH] | pikvm mouse move <x> <y>"))
	}
	screenW, screenH := 1920, 1080
	var coords []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--screen" && i+1 < len(args) {
			parts := strings.SplitN(args[i+1], "x", 2)
			if len(parts) == 2 {
				screenW, _ = strconv.Atoi(parts[0])
				screenH, _ = strconv.Atoi(parts[1])
			}
			i++
			continue
		}
		coords = append(coords, args[i])
	}
	if len(coords) < 3 {
		die(jsonMode, "mouse", fmt.Errorf("usage: pikvm mouse click|move <x> <y> [--screen 1920x1080]"))
	}
	x, err1 := strconv.Atoi(coords[1])
	y, err2 := strconv.Atoi(coords[2])
	if err1 != nil || err2 != nil {
		die(jsonMode, "mouse", fmt.Errorf("x and y must be integers"))
	}
	switch coords[0] {
	case "click":
		if err := api.ClickPixel(x, y, screenW, screenH); err != nil {
			die(jsonMode, "mouse_click", err)
		}
		if jsonMode {
			emit(true, "mouse_click", map[string]interface{}{"x": x, "y": y, "screen": fmt.Sprintf("%dx%d", screenW, screenH)}, nil)
			return
		}
		fmt.Printf("\uf00c Mouse click at (%d,%d) screen %dx%d\n", x, y, screenW, screenH)
	case "move":
		toX, toY := api.PixelToPiKVM(x, y, screenW, screenH)
		if err := api.SendMouseMove(toX, toY); err != nil {
			die(jsonMode, "mouse_move", err)
		}
		if jsonMode {
			emit(true, "mouse_move", map[string]interface{}{"x": x, "y": y, "to_x": toX, "to_y": toY}, nil)
			return
		}
		fmt.Printf("\uf00c Mouse move to (%d,%d)\n", x, y)
	default:
		die(jsonMode, "mouse", fmt.Errorf("unknown mouse subcommand %q", coords[0]))
	}
}
