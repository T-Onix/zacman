// Package execx runs child processes without letting Ctrl+C kill Zacman before
// its foreground child shuts down.
package execx

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
)

// ErrInterrupted reports that the user interrupted a running child process.
var ErrInterrupted = errors.New("interrupted by Ctrl+C")

// Run waits for the child even when Ctrl+C is pressed. The terminal signals the
// whole foreground group, so waiting keeps Zacman from orphaning Pacman mid-shutdown.
func Run(cmd *exec.Cmd) error {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	err := cmd.Run()
	select {
	case <-interrupts:
		return errors.Join(ErrInterrupted, err)
	default:
		return err
	}
}
