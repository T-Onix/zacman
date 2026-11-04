package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/T-Onix/zacman/internal/config"
	"github.com/T-Onix/zacman/internal/services"
	"github.com/T-Onix/zacman/internal/transaction"
)

// showControlConfig displays the persistent startup policy. Runtime start/stop
// actions are intentionally separate from enable/disable policy.
func (a *App) showControlConfig(path string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	policy := servicesWithGreeter(cfg)
	fmt.Fprintln(os.Stdout, paint("lavender", "Zacman")+" "+paint("subtle", "—")+" "+paint("cyan", "systemd controls"))
	fmt.Fprintf(os.Stdout, "Config: %s\n\n", path)
	printUnitPolicy("Enabled on boot (including selected greeter)", policy.Enable, "+", "mint")
	printUnitPolicy("Disabled on boot", policy.Disable, "-", "red")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, paint("subtle", "enable/disable update this policy and call systemctl immediately."))
	fmt.Fprintln(os.Stdout, paint("subtle", "start/stop affect the current runtime only and do not change the TOML policy."))
	return nil
}

func printUnitPolicy(title string, units []string, marker, color string) {
	fmt.Fprintln(os.Stdout, paint("cyan", title)+":")
	if len(units) == 0 {
		fmt.Fprintln(os.Stdout, "  (none)")
		return
	}
	for _, unit := range units {
		fmt.Fprintf(os.Stdout, "  %s %s\n", paint(color, marker), unit)
	}
}

func normalizeCLIUnit(raw string) (string, error) {
	unit := strings.TrimSpace(raw)
	if unit == "" {
		return "", fmt.Errorf("systemd unit name cannot be empty")
	}
	if unit != raw {
		return "", fmt.Errorf("invalid whitespace around systemd unit name %q", raw)
	}
	// Convenience: `zacman ctl enable sshd` means sshd.service. Full systemd
	// unit names such as NetworkManager.service or docker.socket are unchanged.
	if !strings.Contains(unit, ".") {
		unit += ".service"
	}
	if err := config.ValidateServiceUnit(unit); err != nil {
		return "", err
	}
	return unit, nil
}

func (a *App) controlPolicy(path, action, rawUnit string) error {
	unit, err := normalizeCLIUnit(rawUnit)
	if err != nil {
		return err
	}
	if action != "enable" && action != "disable" {
		return fmt.Errorf("unsupported persistent service action %q", action)
	}
	if err := requireRoot("change systemd service policy and update Zacman's configuration"); err != nil {
		return err
	}
	unlock, err := transaction.AcquireLock("")
	if err != nil {
		return err
	}
	defer unlock()

	cfg, err := config.LoadBase(path)
	if err != nil {
		return err
	}
	manager := services.New()
	status, err := manager.IsEnabled(unit)
	if err != nil {
		return err
	}
	if status == services.Missing {
		return fmt.Errorf("%s does not exist; no TOML or service changes were made", unit)
	}
	if status == services.Masked || status == services.Static {
		return fmt.Errorf("cannot %s %s while its systemd state is %s; no TOML changes were made", action, unit, status)
	}

	if action == "enable" {
		cfg.Services.Enable = addUnit(cfg.Services.Enable, unit)
		cfg.Services.Disable = removeUnit(cfg.Services.Disable, unit)
	} else {
		if isCriticalUnit(unit) {
			fmt.Fprintf(os.Stderr, "%s Disabling %s may disrupt networking, remote access, login, or core system operation.\n", paint("red", "Warning:"), unit)
		}
		cfg.Services.Disable = addUnit(cfg.Services.Disable, unit)
		cfg.Services.Enable = removeUnit(cfg.Services.Enable, unit)
	}
	// Validate the updated policy against referenced inventory settings too;
	// for example, an imported greeter cannot also be placed in services.disable.
	effective, err := config.Load(path)
	if err != nil {
		return err
	}
	effective.Services = cfg.Services
	if err := config.Validate(effective); err != nil {
		return fmt.Errorf("service policy conflicts with the effective Zacman configuration: %w", err)
	}
	if err := config.WriteAtomic(path, cfg); err != nil {
		return fmt.Errorf("could not update %s; systemd was not changed: %w", path, err)
	}

	var actionErr error
	if action == "enable" {
		actionErr = manager.Enable(unit)
	} else {
		actionErr = manager.Disable(unit)
	}
	if actionErr != nil {
		return fmt.Errorf("policy was saved to %s but systemctl %s %s failed; retry with 'sudo zacman init': %w", path, action, unit, actionErr)
	}
	if action == "enable" {
		fmt.Fprintf(os.Stdout, "%s Enabled %s and recorded it in services.enable.\n", paint("mint", "✓"), unit)
	} else {
		fmt.Fprintf(os.Stdout, "%s Disabled %s and recorded it in services.disable.\n", paint("mint", "✓"), unit)
	}
	return nil
}

func (a *App) controlRuntime(action, rawUnit string) error {
	unit, err := normalizeCLIUnit(rawUnit)
	if err != nil {
		return err
	}
	if action != "status" {
		if err := requireRoot("change the runtime state of systemd units"); err != nil {
			return err
		}
		unlock, err := transaction.AcquireLock("")
		if err != nil {
			return err
		}
		defer unlock()
	}
	manager := services.New()
	switch action {
	case "start":
		err = manager.Start(unit)
	case "stop":
		err = manager.Stop(unit)
	case "status":
		err = manager.Status(unit)
	default:
		return fmt.Errorf("unsupported runtime service action %q", action)
	}
	if err != nil {
		return err
	}
	if action != "status" {
		fmt.Fprintf(os.Stdout, "%s %s %s. The TOML startup policy was not changed.\n", paint("mint", "✓"), strings.ToUpper(action[:1])+action[1:], unit)
	}
	return nil
}

func addUnit(values []string, unit string) []string {
	out := append([]string(nil), values...)
	found := false
	for _, value := range out {
		if value == unit {
			found = true
			break
		}
	}
	if !found {
		out = append(out, unit)
	}
	sort.Strings(out)
	return out
}

func removeUnit(values []string, unit string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != unit {
			out = append(out, value)
		}
	}
	return out
}
