package main

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/T-Onix/zacman/internal/cli"
	"github.com/T-Onix/zacman/internal/execx"
)

var version = "dev"

func main() {
	app := cli.New(resolvedVersion())
	if err := app.Run(os.Args[1:]); err != nil {
		if errors.Is(err, execx.ErrInterrupted) {
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, "zacman:", err)
		// Direct Pacman passthrough should preserve Pacman's real exit status.
		// This also preserves a wrapped Pacman exit status from Zacman-managed
		// operations instead of flattening every failure to status 1.
		var exitCoder interface{ ExitCode() int }
		if errors.As(err, &exitCoder) {
			code := exitCoder.ExitCode()
			if code > 0 && code < 256 {
				os.Exit(code)
			}
		}
		os.Exit(1)
	}
}

func resolvedVersion() string {
	if version != "" && version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}
