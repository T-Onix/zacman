package cli

import (
	"fmt"
	"os"
	"strings"
)

var noColorOverride bool

// paint uses a small Catppuccin-style palette and stays plain when output is
// redirected or NO_COLOR/TERM=dumb says no ANSI.
func paint(kind, value string) string {
	if noColorOverride || !colorEnabled() {
		return value
	}
	code := ""
	switch kind {
	case "lavender":
		code = "1;38;2;203;166;247"
	case "mint":
		code = "1;38;2;166;227;161"
	case "yellow":
		code = "1;38;2;249;226;175"
	case "red":
		code = "1;38;2;243;139;168"
	case "cyan":
		code = "1;38;2;137;220;235"
	case "subtle":
		code = "38;2;166;173;200"
	default:
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func countLabel(kind, label string, count int) string {
	return paint(kind, fmt.Sprintf("%s: %d", label, count))
}

func colorEnabled() bool {
	if _, exists := os.LookupEnv("NO_COLOR"); exists || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	if os.Getenv("CLICOLOR_FORCE") != "" || os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
