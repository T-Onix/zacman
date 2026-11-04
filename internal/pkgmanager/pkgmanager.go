// Package pkgmanager provides safe, shell-free adapters for non-Pacman package
// managers used by Zacman. Pacman remains the owner of Arch repository packages.
package pkgmanager

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Kind string

const (
	Flatpak Kind = "flatpak"
	UV      Kind = "uv"
	Pip3    Kind = "pip3"
	NPM     Kind = "npm"
	Cargo   Kind = "cargo"
	Go      Kind = "go"
)

var (
	validPackageRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._+:/<>=!~^,-]*$`)
	validScopedNPMRE = regexp.MustCompile(`^@[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._+~^-]*(@[A-Za-z0-9._+~-]+)?$`)
)

func ValidatePackage(kind Kind, value string) error {
	valid := validPackageRE.MatchString(value)
	if kind == NPM && validScopedNPMRE.MatchString(value) {
		valid = true
	}
	if value == "" || strings.TrimSpace(value) != value || strings.HasPrefix(value, "-") || !valid {
		return fmt.Errorf("invalid %s package specifier %q", kind, value)
	}
	if strings.ContainsAny(value, "\r\n\x00;&|`$(){}\\") {
		return fmt.Errorf("invalid %s package specifier %q", kind, value)
	}
	return nil
}

func Available(kind Kind) bool {
	return binaryAvailable(binaryFor(kind), true)
}

func binaryAvailable(name string, userContext bool) bool {
	if _, err := exec.LookPath(name); err == nil {
		return true
	}
	if !userContext || os.Geteuid() != 0 {
		return false
	}
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" || sudoUser == "root" {
		return false
	}
	account, err := user.Lookup(sudoUser)
	if err != nil {
		return false
	}
	candidates := map[string][]string{
		"uv":      {filepath.Join(account.HomeDir, ".local/bin/uv")},
		"cargo":   {filepath.Join(account.HomeDir, ".cargo/bin/cargo")},
		"npm":     {filepath.Join(account.HomeDir, ".npm-global/bin/npm"), filepath.Join(account.HomeDir, ".local/bin/npm")},
		"go":      {filepath.Join(account.HomeDir, "go/bin/go")},
		"python3": {filepath.Join(account.HomeDir, ".local/bin/python3")},
	}
	for _, candidate := range candidates[name] {
		info, statErr := os.Stat(candidate)
		if statErr == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return true
		}
	}
	return false
}

func binaryFor(kind Kind) string {
	switch kind {
	case Flatpak:
		return "flatpak"
	case UV:
		return "uv"
	case Pip3:
		return "python3"
	case NPM:
		return "npm"
	case Cargo:
		return "cargo"
	case Go:
		return "go"
	default:
		return string(kind)
	}
}

// Installed returns the top-level packages tracked by a manager. User-scoped
// managers run as the invoking user when Zacman was started through sudo.
func Installed(kind Kind, scope, remote string) ([]string, error) {
	var name string
	var args []string
	userContext := kind != Flatpak || scope == "user"
	switch kind {
	case Flatpak:
		args = []string{"list", "--app", "--columns=application"}
		if scope == "user" {
			args = append(args, "--user")
		} else {
			args = append(args, "--system")
		}
		name = "flatpak"
	case UV:
		name, args = "uv", []string{"tool", "list"}
	case Pip3:
		name, args = "python3", []string{"-m", "pip", "list", "--user", "--format=json"}
	case NPM:
		name, args = "npm", []string{"ls", "--global", "--depth=0", "--json"}
	case Cargo:
		name, args = "cargo", []string{"install", "--list"}
	case Go:
		return installedGoModules(userContext)
	default:
		return nil, fmt.Errorf("unsupported package manager %q", kind)
	}
	out, err := output(name, args, userContext)
	if err != nil {
		// npm ls exits non-zero when the tree contains warnings or unmet optional
		// dependencies. Its JSON can still be valid and useful to inventory.
		if kind != NPM || len(bytes.TrimSpace(out)) == 0 {
			return nil, fmt.Errorf("query %s installed packages: %w", kind, err)
		}
	}
	return parseInstalled(kind, out)
}

// parseInstalled converts each manager's native inventory output into canonical
// package selectors. Keeping parsing pure allows fixture tests to cover manager
// output changes without running a real package manager.
func parseInstalled(kind Kind, out []byte) ([]string, error) {
	var values []string
	switch kind {
	case Flatpak:
		for _, line := range strings.Split(string(out), "\n") {
			value := strings.TrimSpace(line)
			lower := strings.ToLower(value)
			if value == "" || lower == "application" || lower == "name" || lower == "no applications installed" || lower == "no installed apps" {
				continue
			}
			values = append(values, value)
		}
	case UV:
		// `uv tool list` starts each tool with `name vX.Y`; command entries
		// are indented or start with a dash and are not tool names.
		for _, line := range strings.Split(string(out), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "-") || strings.EqualFold(trimmed, "No tools installed") {
				continue
			}
			fields := strings.Fields(trimmed)
			if len(fields) > 0 {
				values = append(values, fields[0])
			}
		}
	case Pip3:
		var pkgs []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(out, &pkgs); err != nil {
			return nil, fmt.Errorf("parse pip3 package list: %w", err)
		}
		for _, pkg := range pkgs {
			if pkg.Name != "" {
				values = append(values, pkg.Name)
			}
		}
	case NPM:
		var root struct {
			Dependencies map[string]json.RawMessage `json:"dependencies"`
		}
		if err := json.Unmarshal(out, &root); err != nil {
			return nil, fmt.Errorf("parse npm package list: %w", err)
		}
		for pkg := range root.Dependencies {
			values = append(values, pkg)
		}
	case Cargo:
		for _, line := range strings.Split(string(out), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasSuffix(trimmed, ":") {
				continue
			}
			fields := strings.Fields(strings.TrimSuffix(trimmed, ":"))
			if len(fields) > 0 {
				values = append(values, fields[0])
			}
		}
	default:
		return nil, fmt.Errorf("unsupported package manager %q", kind)
	}
	return unique(values), nil
}

func IsInstalled(kind Kind, installed []string, wanted string) bool {
	want := canonical(kind, wanted)
	for _, item := range installed {
		if canonical(kind, item) == want {
			return true
		}
		// Go metadata often exposes a versioned module path while config can use
		// @latest. Match module path irrespective of its requested version.
		if kind == Go && beforeAt(item) == beforeAt(wanted) {
			return true
		}
	}
	return false
}

func canonical(kind Kind, value string) string {
	switch kind {
	case Pip3:
		value = strings.ToLower(strings.NewReplacer("_", "-", ".", "-").Replace(beforeComparator(value)))
	case Go:
		value = beforeAt(value)
	case Cargo:
		value = beforeAt(value)
	case UV:
		value = strings.ToLower(beforeVersion(beforeComparator(value)))
	case NPM:
		value = strings.ToLower(beforeComparator(value))
		if strings.HasPrefix(value, "@") {
			if slash := strings.IndexByte(value, '/'); slash >= 0 {
				if version := strings.IndexByte(value[slash+1:], '@'); version >= 0 {
					value = value[:slash+1+version]
				}
			}
		} else if version := strings.IndexByte(value, '@'); version > 0 {
			value = value[:version]
		}
	}
	return value
}

func beforeAt(value string) string {
	if i := strings.LastIndexByte(value, '@'); i > 0 && !strings.Contains(value[:i], "://") {
		return value[:i]
	}
	return value
}

func beforeVersion(value string) string {
	if i := strings.Index(value, " v"); i > 0 {
		return value[:i]
	}
	return value
}

func beforeComparator(value string) string {
	for _, marker := range []string{">=", "<=", "==", "!=", ">", "<", "~", "!"} {
		if i := strings.Index(value, marker); i > 0 {
			return value[:i]
		}
	}
	return value
}

func Install(kind Kind, pkg, remote, scope string) error {
	if err := ValidatePackage(kind, pkg); err != nil {
		return err
	}
	name, args, userContext := "", []string{}, true
	switch kind {
	case Flatpak:
		name = "flatpak"
		if scope == "user" {
			args = []string{"install", "--user", remote, pkg}
		} else {
			args = []string{"install", "--system", remote, pkg}
			userContext = false
		}
	case UV:
		name, args = "uv", []string{"tool", "install", pkg}
	case Pip3:
		// Keep Python packages out of Arch's externally managed system Python.
		// `pip --user` is non-destructive; if PEP 668 refuses it, report that and
		// recommend uv rather than bypassing the protection with break-system-packages.
		name, args = "python3", []string{"-m", "pip", "install", "--user", pkg}
	case NPM:
		name, args = "npm", []string{"install", "--global", pkg}
	case Cargo:
		name = "cargo"
		base := beforeAt(pkg)
		if version := afterAt(pkg); version != "" {
			args = []string{"install", base, "--version", version}
		} else {
			args = []string{"install", pkg}
		}
	case Go:
		name = "go"
		if !strings.Contains(pkg, "@") {
			pkg += "@latest"
		}
		args = []string{"install", pkg}
	default:
		return fmt.Errorf("unsupported package manager %q", kind)
	}
	if !Available(kind) {
		return fmt.Errorf("%s is not installed or not available in the invoking user's PATH; add its Arch provider package to packages.repo or install it for the invoking user first", name)
	}
	cmd := command(name, args, userContext)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if kind == Pip3 {
			return fmt.Errorf("pip3 install failed (Arch may enforce an externally-managed Python environment); use uv for CLI tools or a virtual environment: %w", err)
		}
		return fmt.Errorf("%s install %q failed: %w", kind, pkg, err)
	}
	return nil
}

func Update(kind Kind, packages []string, remote, scope string) error {
	// Managers with per-tool version semantics are invoked once per package so
	// every argument is interpreted by the proper native subcommand.
	if kind == Cargo || kind == Go {
		if len(packages) == 0 {
			return fmt.Errorf("%s requires explicit package names for updates", kind)
		}
		for _, pkg := range packages {
			if err := ValidatePackage(kind, pkg); err != nil {
				return err
			}
			if kind == Cargo {
				name := beforeAt(pkg)
				args := []string{"install", name, "--force"}
				if version := afterAt(pkg); version != "" {
					args = append(args, "--version", version)
				}
				if err := runInteractive("cargo", args, true); err != nil {
					return fmt.Errorf("cargo update %q failed: %w", pkg, err)
				}
			} else {
				if !strings.Contains(pkg, "@") {
					pkg += "@latest"
				}
				if err := runInteractive("go", []string{"install", pkg}, true); err != nil {
					return fmt.Errorf("go update %q failed: %w", pkg, err)
				}
			}
		}
		return nil
	}
	name, args, userContext := "", []string{}, true
	switch kind {
	case Flatpak:
		name = "flatpak"
		args = []string{"update"}
		if scope == "user" {
			args = append(args, "--user")
		} else {
			args = append(args, "--system")
			userContext = false
		}
		args = append(args, packages...)
	case UV:
		name = "uv"
		args = []string{"tool", "upgrade"}
		if len(packages) == 0 {
			args = append(args, "--all")
		} else {
			args = append(args, packages...)
		}
	case Pip3:
		if len(packages) == 0 {
			return errors.New("pip3 update requires explicit package names; it will not update every package in the user environment")
		}
		name = "python3"
		args = []string{"-m", "pip", "install", "--user", "--upgrade"}
		args = append(args, packages...)
	case NPM:
		name = "npm"
		args = []string{"update", "--global"}
		args = append(args, packages...)
	default:
		return fmt.Errorf("unsupported package manager %q", kind)
	}
	if len(args) == 0 {
		return fmt.Errorf("no update operation was selected for %s", kind)
	}
	if err := runInteractiveAs(name, args, userContext); err != nil {
		return fmt.Errorf("%s update failed: %w", kind, err)
	}
	return nil
}

// Uninstall uses each ecosystem's native remover. Go-installed tools deliberately
// return an error because a module path is not a safe/reliable way to infer which
// binary should be removed from a user's bin directory.
func Uninstall(kind Kind, pkg string, remote, scope string) error {
	if err := ValidatePackage(kind, pkg); err != nil {
		return err
	}
	name, args, userContext := "", []string{}, true
	switch kind {
	case Flatpak:
		name = "flatpak"
		args = []string{"uninstall"}
		if scope == "user" {
			args = append(args, "--user")
		} else {
			args = append(args, "--system")
			userContext = false
		}
		args = append(args, pkg)
	case UV:
		name, args = "uv", []string{"tool", "uninstall", pkg}
	case Pip3:
		name, args = "python3", []string{"-m", "pip", "uninstall", "--yes", pkg}
	case NPM:
		name, args = "npm", []string{"uninstall", "--global", pkg}
	case Cargo:
		name, args = "cargo", []string{"uninstall", beforeAt(pkg)}
	case Go:
		return errors.New("Go modules installed with `go install` do not have a reliable native uninstall command; remove the known binary manually, then remove it from [golang].pkgs")
	default:
		return fmt.Errorf("unsupported package manager %q", kind)
	}
	if err := runInteractiveAs(name, args, userContext); err != nil {
		return fmt.Errorf("%s uninstall %q failed: %w", kind, pkg, err)
	}
	return nil
}

func runInteractive(name string, args []string, userContext bool) error {
	return runInteractiveAs(name, args, userContext)
}

func runInteractiveAs(name string, args []string, userContext bool) error {
	if !binaryAvailable(name, userContext) {
		return fmt.Errorf("%s is not installed or not available in PATH", name)
	}
	cmd := command(name, args, userContext)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func command(name string, args []string, userContext bool) *exec.Cmd {
	if userContext && os.Geteuid() == 0 {
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" && sudoUser != "root" {
			if account, err := user.Lookup(sudoUser); err == nil {
				path := account.HomeDir + "/.local/bin:" + account.HomeDir + "/.cargo/bin:" + account.HomeDir + "/.npm-global/bin:" + account.HomeDir + "/go/bin:/usr/local/bin:/usr/bin:/bin"
				allArgs := []string{"-u", sudoUser, "--", "env", "HOME=" + account.HomeDir, "USER=" + sudoUser, "LOGNAME=" + sudoUser, "PATH=" + path, name}
				allArgs = append(allArgs, args...)
				return exec.Command("runuser", allArgs...)
			}
		}
	}
	return exec.Command(name, args...)
}

func output(name string, args []string, userContext bool) ([]byte, error) {
	cmd := command(name, args, userContext)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && stderr.Len() > 0 {
		return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, err
}

func installedGoModules(userContext bool) ([]string, error) {
	out, err := output("go", []string{"env", "GOBIN", "GOPATH"}, userContext)
	if err != nil {
		return nil, fmt.Errorf("query go environment: %w", err)
	}
	// Do not TrimSpace the whole output before splitting: `go env GOBIN
	// GOPATH` intentionally prints an empty first line when GOBIN is unset.
	// Trimming that newline would mistake GOPATH for GOBIN and skip ~/go/bin.
	lines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
	gobin := ""
	gopath := ""
	if len(lines) > 0 {
		gobin = strings.TrimSpace(lines[0])
	}
	if len(lines) > 1 {
		gopath = strings.TrimSpace(lines[1])
	}
	gobin, gopath = resolveGoInstallPaths(gobin, gopath)
	entries, err := os.ReadDir(gobin)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read Go install directory %q: %w", gobin, err)
	}
	values := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(gobin, entry.Name())
		meta, err := output("go", []string{"version", "-m", path}, userContext)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(meta), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 3 && fields[0] == "mod" {
				mod := fields[1]
				version := fields[2]
				if version != "(devel)" && !strings.HasPrefix(version, "v") {
					version = ""
				}
				if version != "" {
					values = append(values, mod+"@"+version)
				} else {
					values = append(values, mod)
				}
				break
			}
		}
	}
	return unique(values), nil
}

// resolveGoInstallPaths resolves Go's effective binary directory without losing
// the empty GOBIN value emitted when users rely on GOPATH/bin.
func resolveGoInstallPaths(gobin, gopath string) (string, string) {
	gobin = strings.TrimSpace(gobin)
	gopath = strings.TrimSpace(gopath)
	if gobin != "" {
		return filepath.Clean(gobin), gopath
	}
	first := strings.Split(gopath, string(os.PathListSeparator))[0]
	if first == "" {
		home, _ := os.UserHomeDir()
		first = filepath.Join(home, "go")
	}
	return filepath.Join(first, "bin"), gopath
}

func afterAt(value string) string {
	if i := strings.LastIndexByte(value, '@'); i > 0 && !strings.Contains(value[:i], "://") {
		return value[i+1:]
	}
	return ""
}

func unique(values []string) []string {
	set := map[string]bool{}
	out := []string{}
	for _, item := range values {
		item = strings.TrimSpace(item)
		if item == "" || set[item] {
			continue
		}
		set[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
