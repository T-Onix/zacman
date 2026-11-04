package execx

import (
	"errors"
	"os/exec"
	"testing"
)

func TestRunSuccess(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := Run(cmd); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunPreservesChildExitError(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 7")
	err := Run(cmd)
	if err == nil {
		t.Fatal("expected child failure")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Run() error %T does not preserve ExitError: %v", err, err)
	}
	if exitErr.ExitCode() != 7 {
		t.Fatalf("exit code = %d, want 7", exitErr.ExitCode())
	}
}
