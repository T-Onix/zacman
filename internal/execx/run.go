// Package execx runs child processes without allowing Ctrl+C to kill Zacman
// before its foreground child has had a chance to shut down.
package execx

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
)

// ErrInterrupted reports that the user interrupted a running child process.
var ErrInterrupted = errors.New("interrupted by Ctrl+C")

// Run waits for the child even when Ctrl+C is pressed. In a normal terminal,
// the interrupt is delivered to the foreground process group, so both Zacman
// and its child receive it. Notify prevents Zacman from exiting immediately
// and orphaning Pacman; the child can handle SIGINT and terminate cleanly.
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
