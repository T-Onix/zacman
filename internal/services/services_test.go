package services

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeSystemctl(t *testing.T, output string, exitCode int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "systemctl")
	script := "#!/bin/sh\nprintf '%s\\n' '" + output + "'\nexit " + string(rune('0'+exitCode)) + "\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIsEnabledRecognizesRuntimeEnabledSeparately(t *testing.T) {
	manager := Manager{Binary: fakeSystemctl(t, "enabled-runtime", 0)}
	got, err := manager.IsEnabled("foo.service")
	if err != nil {
		t.Fatal(err)
	}
	if got != EnabledRuntime {
		t.Fatalf("status = %q, want %q", got, EnabledRuntime)
	}
}

func TestIsEnabledRecognizesDisabledDespiteExitCodeOne(t *testing.T) {
	manager := Manager{Binary: fakeSystemctl(t, "disabled", 1)}
	got, err := manager.IsEnabled("foo.service")
	if err != nil {
		t.Fatal(err)
	}
	if got != Disabled {
		t.Fatalf("status = %q, want %q", got, Disabled)
	}
}

func TestStartStopAndStatusForwardStructuredArguments(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "args")
	path := filepath.Join(dir, "systemctl")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$SYSTEMCTL_CAPTURE\"\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SYSTEMCTL_CAPTURE", capture)
	manager := Manager{Binary: path}
	if err := manager.Start("demo.service"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop("demo.service"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Status("demo.service"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	want := "start\ndemo.service\nstop\ndemo.service\nstatus\n--no-pager\ndemo.service\n"
	if string(data) != want {
		t.Fatalf("systemctl args = %q, want %q", data, want)
	}
}
