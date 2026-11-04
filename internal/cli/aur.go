package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/T-Onix/zacman/internal/execx"
	"github.com/T-Onix/zacman/internal/pacman"
)

const defaultAURRPCBase = "https://aur.archlinux.org"

type aurRPCResponse struct {
	Version     int    `json:"version"`
	Type        string `json:"type"`
	ResultCount int    `json:"resultcount"`
	Results     []struct {
		Name string `json:"Name"`
	} `json:"results"`
}

// lookupAURPackages checks exact package names through the official AUR RPC v5
// info endpoint. AUR lookup is read-only; package builds still require an
// explicit user decision and are run as the invoking unprivileged user.
func lookupAURPackages(names []string) (map[string]bool, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	return lookupAURPackagesAt(context.Background(), client, defaultAURRPCBase, names)
}

func lookupAURPackagesAt(ctx context.Context, client *http.Client, baseURL string, names []string) (map[string]bool, error) {
	found := make(map[string]bool)
	if len(names) == 0 {
		return found, nil
	}
	values := url.Values{}
	for _, name := range uniqueAURPackages(names) {
		if err := validateAURPackage(name); err != nil {
			return nil, err
		}
		values.Add("arg[]", name)
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/rpc/v5/info?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create AUR lookup request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "zacman/1.0 (AUR package availability check)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query the AUR RPC: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("AUR RPC returned HTTP %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var payload aurRPCResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode AUR RPC response: %w", err)
	}
	if payload.Version != 5 || payload.Type != "multiinfo" {
		return nil, fmt.Errorf("unexpected AUR RPC response (version=%d type=%q)", payload.Version, payload.Type)
	}
	requested := make(map[string]bool, len(names))
	for _, name := range names {
		requested[name] = true
	}
	for _, result := range payload.Results {
		if requested[result.Name] {
			found[result.Name] = true
		}
	}
	return found, nil
}

func findAURHelper() string {
	for _, name := range []string{"yay", "paru"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func parseAURBatch(values []string) ([]string, bool, error) {
	if len(values) == 0 {
		return nil, false, nil
	}
	packages := []string{}
	found := false
	for _, value := range values {
		if strings.HasPrefix(strings.ToLower(value), "aur:") {
			found = true
			name := strings.TrimSpace(value[len("aur:"):])
			if err := validateAURPackage(name); err != nil {
				return nil, true, err
			}
			packages = append(packages, name)
		}
	}
	if !found {
		return nil, false, nil
	}
	if len(packages) != len(values) {
		return nil, true, errors.New("do not mix aur: package specifiers with Pacman or other package-manager targets; run separate commands")
	}
	return uniqueAURPackages(packages), true, nil
}

func validateAURPackage(name string) error {
	if name == "" || strings.TrimSpace(name) != name || strings.ContainsAny(name, "/\\\x00\r\n\t ") || strings.HasPrefix(name, "-") {
		return fmt.Errorf("invalid AUR package name %q", name)
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("@._+-", r)) {
			return fmt.Errorf("invalid character %q in AUR package name %q", r, name)
		}
	}
	return nil
}

func uniqueAURPackages(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

// canonicalAURPackageName maps documented project/binary selectors to the
// actual AUR package name. AUR manifests should normally use the PKGBUILD's
// pkgname; aliases are kept for compatibility with earlier Zacman manifests.
func canonicalAURPackageName(name string) string {
	switch name {
	case "ghgrab":
		return "ghgrab-bin"
	default:
		return name
	}
}

func canonicalAURPackages(values []string) []string {
	canonical := make([]string, 0, len(values))
	for _, value := range values {
		canonical = append(canonical, canonicalAURPackageName(value))
	}
	return uniqueAURPackages(canonical)
}

// missingAURPackageTargets returns real AUR package names which are not
// installed. Compatibility aliases are checked against both their old and
// canonical names to avoid repeatedly reinstalling a package such as ghgrab-bin
// when the manifest still says ghgrab.
func missingAURPackageTargets(configured, installed []string) []string {
	installedSet := make(map[string]bool, len(installed))
	for _, name := range installed {
		installedSet[name] = true
	}
	missing := make([]string, 0, len(configured))
	for _, requested := range configured {
		target := canonicalAURPackageName(requested)
		if installedSet[requested] || installedSet[target] {
			continue
		}
		missing = append(missing, target)
	}
	return uniqueAURPackages(missing)
}

type invokingUser struct {
	name string
	home string
	uid  uint32
	gid  uint32
}

func getInvokingUser() (invokingUser, error) {
	uidText := strings.TrimSpace(os.Getenv("SUDO_UID"))
	if uidText == "" || uidText == "0" {
		return invokingUser{}, errors.New("AUR builds must run as your normal user, not root; invoke Zacman with sudo from your user session so it can use SUDO_UID")
	}
	uidValue, err := strconv.ParseUint(uidText, 10, 32)
	if err != nil {
		return invokingUser{}, fmt.Errorf("invalid SUDO_UID %q: %w", uidText, err)
	}
	account, err := user.LookupId(uidText)
	if err != nil {
		return invokingUser{}, fmt.Errorf("look up invoking user with UID %s: %w", uidText, err)
	}
	gidValue, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil {
		return invokingUser{}, fmt.Errorf("invalid primary GID for %s: %w", account.Username, err)
	}
	if account.HomeDir == "" || account.HomeDir == "/" {
		return invokingUser{}, fmt.Errorf("refusing to build AUR packages with unsafe home directory %q", account.HomeDir)
	}
	return invokingUser{name: account.Username, home: account.HomeDir, uid: uint32(uidValue), gid: uint32(gidValue)}, nil
}

func runAsInvokingUser(account invokingUser, directory, binary string, args ...string) error {
	runuser, err := exec.LookPath("runuser")
	if err != nil {
		return errors.New("runuser is required to build/install AUR helpers as the invoking unprivileged user")
	}
	pathValue := os.Getenv("PATH")
	if pathValue == "" {
		pathValue = "/usr/local/bin:/usr/bin:/bin"
	}
	envArgs := []string{
		"-u", account.name, "--", "env",
		"-u", "SUDO_UID", "-u", "SUDO_GID", "-u", "SUDO_USER", "-u", "SUDO_COMMAND",
		"HOME=" + account.home, "USER=" + account.name, "LOGNAME=" + account.name, "PATH=" + pathValue,
		binary,
	}
	cmd := exec.Command(runuser, append(envArgs, args...)...)
	if directory != "" {
		cmd.Dir = directory
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := execx.Run(cmd); err != nil {
		return fmt.Errorf("%s failed as unprivileged user %s: %w", binary, account.name, err)
	}
	return nil
}

// installAURPackages is intentionally limited to a helper-backed AUR workflow.
// It never runs makepkg as root and never evaluates PKGBUILD values as shell text.
func installAURPackages(packages []string, dontUpdate bool) error {
	return installAURPackagesMode(packages, !dontUpdate)
}

// installAURPackagesSynced is used by init after its own optional `pacman -Sy`.
// It must not run yay/paru -Syu because init is intentionally install-only.
func installAURPackagesSynced(packages []string) error {
	return installAURPackagesMode(packages, false)
}

func installAURPackagesMode(packages []string, fullUpgrade bool) error {
	if len(packages) == 0 {
		return nil
	}
	if os.Geteuid() != 0 {
		return errors.New("AUR package installation requires root for Pacman transactions; run 'sudo zacman init' from your normal user session")
	}
	account, err := getInvokingUser()
	if err != nil {
		return err
	}
	helper := findAURHelper()
	if helper == "" {
		if err := bootstrapYayMode(account, fullUpgrade); err != nil {
			return fmt.Errorf("bootstrap AUR helper yay: %w", err)
		}
		helper = findAURHelper()
		if helper == "" {
			return errors.New("yay bootstrap finished but neither yay nor paru is available in PATH")
		}
	}
	args := aurInstallArgs(packages, fullUpgrade)
	fmt.Fprintf(os.Stdout, "%s Installing AUR packages via %s as %s\n", paint("mint", "→"), filepath.Base(helper), account.name)
	return runAsInvokingUser(account, account.home, helper, args...)
}

func aurInstallArgs(packages []string, fullUpgrade bool) []string {
	args := []string{"-S", "--needed"}
	if fullUpgrade {
		args[0] = "-Syu"
	}
	return append(args, packages...)
}

func bootstrapYay(account invokingUser, dontUpdate bool) error {
	return bootstrapYayMode(account, !dontUpdate)
}

func bootstrapYayMode(account invokingUser, fullUpgrade bool) error {
	// base-devel is an Arch package group; pacman --needed makes this safe
	// whether some or all of its members are already installed.
	prerequisites := []string{"base-devel", "git", "go"}
	fmt.Fprintf(os.Stdout, "%s Ensuring AUR build prerequisites are installed: %s\n", paint("cyan", "→"), strings.Join(prerequisites, ", "))
	var installErr error
	if fullUpgrade {
		installErr = pacman.New().Install(prerequisites, false, nil)
	} else {
		installErr = pacman.New().InstallSynced(prerequisites)
	}
	if installErr != nil {
		return fmt.Errorf("install AUR build prerequisites: %w", installErr)
	}
	base, err := os.MkdirTemp("/tmp", "zacman-yay-build-")
	if err != nil {
		return fmt.Errorf("create temporary build directory: %w", err)
	}
	defer os.RemoveAll(base)
	if err := os.Chown(base, int(account.uid), int(account.gid)); err != nil {
		return fmt.Errorf("make temporary build directory owned by %s: %w", account.name, err)
	}
	if err := os.Chmod(base, 0700); err != nil {
		return err
	}
	repoPath := filepath.Join(base, "yay")
	if err := runAsInvokingUser(account, base, "git", "clone", "--depth=1", "https://aur.archlinux.org/yay.git", repoPath); err != nil {
		return fmt.Errorf("clone yay PKGBUILD from the AUR: %w", err)
	}
	if err := os.Chown(repoPath, int(account.uid), int(account.gid)); err != nil {
		return fmt.Errorf("set yay source directory ownership: %w", err)
	}
	fmt.Fprintln(os.Stdout, paint("cyan", "→ Building yay as an unprivileged user. Review AUR build output and PKGBUILD trust before using AUR packages."))
	if err := runAsInvokingUser(account, repoPath, "makepkg", "--cleanbuild", "--noconfirm"); err != nil {
		return fmt.Errorf("build yay from its AUR PKGBUILD: %w", err)
	}
	matches, err := filepath.Glob(filepath.Join(repoPath, "yay-*.pkg.tar.zst"))
	if err != nil {
		return err
	}
	packages := []string{}
	for _, match := range matches {
		name := filepath.Base(match)
		if strings.HasPrefix(name, "yay-") && !strings.HasPrefix(name, "yay-debug-") && strings.HasSuffix(name, ".pkg.tar.zst") {
			packages = append(packages, match)
		}
	}
	if len(packages) == 0 {
		return errors.New("makepkg completed but no yay package archive was created")
	}
	sort.Strings(packages)
	archive := packages[len(packages)-1]
	fmt.Fprintln(os.Stdout, paint("cyan", "→ Installing the built yay package with Pacman."))
	if err := pacman.New().Passthrough("-U", archive); err != nil {
		return fmt.Errorf("install built yay package: %w", err)
	}
	return nil
}
