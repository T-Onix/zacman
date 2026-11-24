package pacman

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/T-Onix/zacman/internal/execx"
)

type Client struct {
	Binary string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func New() Client {
	return Client{Binary: "pacman", Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
}

func (c Client) binary() string {
	if c.Binary == "" {
		return "pacman"
	}
	return c.Binary
}

func (c Client) output(args ...string) ([]byte, error) {
	cmd := exec.Command(c.binary(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := execx.Run(cmd); err != nil {
		// Pacman exits 1 on a query with no matches. Treat that as empty only when it
		// printed nothing; never mask an interrupt or a real database error.
		var exitErr *exec.ExitError
		if !errors.Is(err, execx.ErrInterrupted) && errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && stdout.Len() == 0 && strings.TrimSpace(stderr.String()) == "" {
			return stdout.Bytes(), nil
		}
		if stderr.Len() != 0 {
			return stdout.Bytes(), fmt.Errorf("%s %s: %w: %s", c.binary(), strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
		}
		return stdout.Bytes(), fmt.Errorf("%s %s: %w", c.binary(), strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

func (c Client) run(args ...string) error {
	if err := checkDatabaseLock("/var/lib/pacman/db.lck"); err != nil {
		return err
	}
	cmd := exec.Command(c.binary(), args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.Stdin, c.Stdout, c.Stderr
	if err := execx.Run(cmd); err != nil {
		return fmt.Errorf("%s %s failed: %w", c.binary(), strings.Join(args, " "), err)
	}
	return nil
}

func (c Client) InstalledPackages() ([]string, error) {
	out, err := c.output("-Qq")
	if err != nil {
		return nil, fmt.Errorf("query installed packages: %w", err)
	}
	return strings.Fields(string(out)), nil
}

// SatisfiedByInstalledProviders returns targets satisfied by the local Pacman DB,
// including Provides aliases; pass only targets missing from InstalledPackages().
func (c Client) SatisfiedByInstalledProviders(targets []string) ([]string, error) {
	if len(targets) == 0 {
		return []string{}, nil
	}
	requested := make(map[string]bool, len(targets))
	args := []string{"-T"}
	for _, target := range targets {
		if target == "" || strings.TrimSpace(target) != target || strings.HasPrefix(target, "-") || strings.ContainsAny(target, "\r\n\x00\t ") {
			return nil, fmt.Errorf("invalid package target for dependency test: %q", target)
		}
		if requested[target] {
			continue
		}
		requested[target] = true
		args = append(args, target)
	}
	if len(args) == 1 {
		return []string{}, nil
	}
	cmd := exec.Command(c.binary(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := execx.Run(cmd)
	if errors.Is(err, execx.ErrInterrupted) {
		return nil, fmt.Errorf("dependency test interrupted: %w", err)
	}
	fields := strings.Fields(stdout.String())
	unsatisfied := make(map[string]bool, len(fields))
	for _, name := range fields {
		if !requested[name] {
			return nil, fmt.Errorf("%s -T returned unexpected target %q: %s", c.binary(), name, strings.TrimSpace(stdout.String()))
		}
		unsatisfied[name] = true
	}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || len(fields) == 0 || strings.TrimSpace(stderr.String()) != "" {
			if stderr.Len() > 0 {
				return nil, fmt.Errorf("%s %s: %w: %s", c.binary(), strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
			}
			return nil, fmt.Errorf("%s %s: %w", c.binary(), strings.Join(args, " "), err)
		}
		// Pacman -T returns non-zero when one or more requested dependencies
		// are unsatisfied. Accept this only when stdout enumerates known targets.
	}

	satisfied := make([]string, 0, len(requested)-len(unsatisfied))
	for _, target := range targets {
		if requested[target] && !unsatisfied[target] {
			satisfied = append(satisfied, target)
			delete(requested, target)
		}
	}
	return satisfied, nil
}

// ExplicitNativePackages lists explicitly installed packages from sync repos.
// Foreign packages are never labeled AUR.
func (c Client) ExplicitNativePackages() ([]string, error) {
	out, err := c.output("-Qqen")
	if err != nil {
		return nil, fmt.Errorf("query explicitly installed repository packages: %w", err)
	}
	return strings.Fields(string(out)), nil
}

// ExplicitForeignPackages lists explicitly installed packages from outside the
// sync repos. Dependency-only packages are left out of Zacman's desired inventory.
func (c Client) ExplicitForeignPackages() ([]string, error) {
	out, err := c.output("-Qqem")
	if err != nil {
		return nil, fmt.Errorf("query explicitly installed foreign packages: %w", err)
	}
	return strings.Fields(string(out)), nil
}

// ForeignPackages reports installed packages outside the configured sync repos.
// It does not assume they are AUR.
func (c Client) ForeignPackages() ([]string, error) {
	// Pacman exits 1 when -Qqm has no matches. Empty stdout and stderr then mean
	// no foreign packages, not a database failure.
	cmd := exec.Command(c.binary(), "-Qqm")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := execx.Run(cmd)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 && stdout.Len() == 0 && strings.TrimSpace(stderr.String()) == "" {
			return []string{}, nil
		}
		if stderr.Len() != 0 {
			return nil, fmt.Errorf("query foreign packages: %s -Qqm: %w: %s", c.binary(), err, strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("query foreign packages: %s -Qqm: %w", c.binary(), err)
	}
	return strings.Fields(stdout.String()), nil
}

// SyncDatabases refreshes sync databases without upgrading anything. -Sy can leave
// a partial-upgrade state; full upgrades are exposed separately as `zacman update`.
func (c Client) SyncDatabases() error {
	return c.run("-Sy")
}

// SyncRepositoryPackages returns package names currently present in configured
// sync repositories. Call SyncDatabases first when a fresh view is required.
func (c Client) SyncRepositoryPackages() ([]string, error) {
	out, err := c.output("-Slq")
	if err != nil {
		return nil, fmt.Errorf("list sync-repository packages: %w", err)
	}
	return strings.Fields(string(out)), nil
}

// InstallSynced installs only the requested targets and never upgrades the
// rest of the system. It is for init after an optional explicit -Sy refresh.
func (c Client) InstallSynced(packages []string) error {
	if len(packages) == 0 {
		return nil
	}
	args := []string{"-S", "--needed", "--"}
	args = append(args, packages...)
	return c.run(args...)
}

// InstallArgs chooses a safe full-system upgrade by default. dontUpdate is an
// explicit opt-out requested by the user; it uses -S without refreshing databases.
func InstallArgs(packages []string, dontUpdate bool, ignored []string) []string {
	mode := "-Syu"
	if dontUpdate {
		mode = "-S"
	}
	args := []string{mode}
	if !dontUpdate && len(ignored) > 0 {
		explicit := make(map[string]bool, len(packages))
		for _, pkg := range packages {
			explicit[pkg] = true
		}
		filtered := make([]string, 0, len(ignored))
		for _, pkg := range ignored {
			if !explicit[pkg] {
				filtered = append(filtered, pkg)
			}
		}
		if len(filtered) > 0 {
			args = append(args, "--ignore", strings.Join(filtered, ","))
		}
	}
	if len(packages) > 0 {
		args = append(args, "--")
		args = append(args, packages...)
	}
	return args
}

// UpdateArgs always does a full upgrade to avoid Arch partial upgrades. Extra
// package names are targeted too, but the rest of the system still upgrades.
func UpdateArgs(packages, ignored []string) []string {
	return InstallArgs(packages, false, ignored)
}

func RemoveArgs(packages []string) []string {
	args := []string{"-R", "--"}
	return append(args, packages...)
}

func (c Client) Install(packages []string, dontUpdate bool, ignored []string) error {
	if len(packages) == 0 {
		return nil
	}
	return c.run(InstallArgs(packages, dontUpdate, ignored)...)
}

func (c Client) Update(packages, ignored []string) error {
	return c.run(UpdateArgs(packages, ignored)...)
}

func (c Client) Remove(packages []string) error {
	if len(packages) == 0 {
		return nil
	}
	return c.run(RemoveArgs(packages)...)
}

// Passthrough runs Pacman with the caller's exact args and streams, without
// touching the manifest, ignore list, or Zacman lock. Pacman owns its own lock.
func (c Client) Passthrough(args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("no Pacman arguments supplied; run 'zacman help' for Zacman commands")
	}
	cmd := exec.Command(c.binary(), args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.Stdin, c.Stdout, c.Stderr
	if err := execx.Run(cmd); err != nil {
		return fmt.Errorf("pacman %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

func checkDatabaseLock(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("Pacman database lock exists at %s; another package transaction may be running. Wait for it to finish; Zacman will not remove the lock", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot inspect Pacman database lock %s: %w", path, err)
	}
	return nil
}
