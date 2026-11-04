package services

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/T-Onix/zacman/internal/execx"
)

type Status string

const (
	Enabled        Status = "enabled"
	EnabledRuntime Status = "enabled-runtime"
	Disabled       Status = "disabled"
	Masked         Status = "masked"
	Static         Status = "static"
	Missing        Status = "not-found"
	Unknown        Status = "unknown"
)

type Manager struct {
	Binary string
	Stdin  *os.File
	Stdout *os.File
	Stderr *os.File
}

func New() Manager {
	return Manager{Binary: "systemctl", Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
}

func (m Manager) binary() string {
	if m.Binary == "" {
		return "systemctl"
	}
	return m.Binary
}

func (m Manager) IsEnabled(unit string) (Status, error) {
	cmd := exec.Command(m.binary(), "is-enabled", unit)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := execx.Run(cmd)
	value := strings.TrimSpace(out.String())
	switch value {
	case "enabled", "linked":
		return Enabled, nil
	case "enabled-runtime", "linked-runtime":
		return EnabledRuntime, nil
	case "disabled":
		return Disabled, nil
	case "masked", "masked-runtime":
		return Masked, nil
	case "static", "indirect", "generated", "transient", "alias":
		return Static, nil
	case "not-found", "bad-unit-file":
		return Missing, nil
	}
	if err == nil {
		return Unknown, fmt.Errorf("systemctl is-enabled %s returned unexpected output %q", unit, value)
	}
	if strings.Contains(stderr.String(), "not found") || strings.Contains(stderr.String(), "could not be found") {
		return Missing, nil
	}
	return Unknown, fmt.Errorf("check unit %s: %w: %s", unit, err, strings.TrimSpace(stderr.String()+" "+value))
}

func (m Manager) Enable(unit string) error  { return m.run("enable", unit) }
func (m Manager) Disable(unit string) error { return m.run("disable", unit) }
func (m Manager) Start(unit string) error   { return m.run("start", unit) }
func (m Manager) Stop(unit string) error    { return m.run("stop", unit) }
func (m Manager) Status(unit string) error  { return m.run("status", "--no-pager", unit) }

func (m Manager) run(args ...string) error {
	cmd := exec.Command(m.binary(), args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = m.Stdin, m.Stdout, m.Stderr
	if err := execx.Run(cmd); err != nil {
		return fmt.Errorf("%s %s failed: %w", m.binary(), strings.Join(args, " "), err)
	}
	return nil
}
