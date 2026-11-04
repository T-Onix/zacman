package cli

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/T-Onix/zacman/internal/config"
)

type configCopy struct {
	source, target, display string
	data                    []byte
	mode                    os.FileMode
	owner                   *osuser.User
	needsCopy               bool
}

type systemConfigPlan struct {
	copies          []configCopy
	copyIssues      []string
	localeChanges   []string
	themeChanges    []string
	themeIssues     []string
	themeWarnings   []string
	themeTarget     string // SDDM theme name, or empty for SDDM's embedded fallback.
	themeNeedsCheck bool
	themeRecord     *sddmThemeVerification
	mouseChanges    []string
	mouseIssues     []string
	mouseWarnings   []string
	mouseTarget     string
	mouseDir        string
}

func (p systemConfigPlan) hasChanges() bool {
	if len(p.issues()) > 0 || len(p.localeChanges) > 0 || len(p.themeChanges) > 0 || len(p.mouseChanges) > 0 {
		return true
	}
	for _, item := range p.copies {
		if item.needsCopy {
			return true
		}
	}
	return false
}

func (p systemConfigPlan) issues() []string {
	all := append([]string{}, p.copyIssues...)
	all = append(all, p.themeIssues...)
	all = append(all, p.mouseIssues...)
	sort.Strings(all)
	return all
}

func buildSystemConfigPlan(cfg config.Config, configPath string, pacmanInstallPending bool) systemConfigPlan {
	plan := systemConfigPlan{}
	for _, mapping := range configuredMappings(cfg) {
		item, issue := inspectMapping(configPath, mapping)
		if issue != "" {
			plan.copyIssues = append(plan.copyIssues, issue)
			continue
		}
		if item.needsCopy {
			plan.copies = append(plan.copies, item)
		}
	}
	plan.localeChanges = localeDiff(cfg.Locale)
	if cfg.Greeter.Theme != "" && cfg.Greeter.System == "sddm" {
		themeName, themeDir, resolveErr := config.ResolveSDDMTheme(cfg.Greeter.Theme)
		if resolveErr != nil {
			plan.themeIssues = append(plan.themeIssues, resolveErr.Error())
		} else {
			plan.themeTarget = themeName
			fingerprint := sddmThemeFingerprint(themeDir)
			fallback := safeSDDMFallbackTheme()
			plan.themeRecord = &sddmThemeVerification{
				Desired: cfg.Greeter.Theme, Fingerprint: fingerprint, Outcome: "fallback", Fallback: fallback,
			}

			// A repository theme may not exist until the package transaction has
			// completed. In that case defer validation until apply re-plans.
			canValidate := !(cfg.Greeter.ThemeSource == "repo" && pacmanInstallPending)
			if canValidate {
				if err := validateSDDMThemeDir(themeDir); err != nil {
					plan.themeTarget = fallback
					plan.themeRecord.Reason = err.Error()
					plan.themeWarnings = append(plan.themeWarnings, fmt.Sprintf("configured SDDM theme %q failed the safety check (%v); Zacman will use %s instead", cfg.Greeter.Theme, err, sddmThemeLabel(fallback)))
				} else {
					if record, ok := readSDDMThemeVerification(); ok && record.Desired == cfg.Greeter.Theme && record.Fingerprint == fingerprint {
						plan.themeRecord = &record
						if record.Outcome == "fallback" {
							plan.themeTarget = record.Fallback
							reason := record.Reason
							if reason == "" {
								reason = "the preview was rejected or unavailable"
							}
							plan.themeWarnings = append(plan.themeWarnings, fmt.Sprintf("SDDM theme %q is marked unsafe (%s); keeping %s as the safe fallback. Change the theme files or remove the Zacman verification comment in %s to retry", cfg.Greeter.Theme, reason, sddmThemeLabel(record.Fallback), sddmThemeConfigPath))
						} else if record.Outcome == "approved" {
							plan.themeTarget = themeName
							plan.themeRecord = &record
						} else {
							plan.themeNeedsCheck = themeName != "breeze"
						}
					} else if themeName != "breeze" {
						// A structurally valid QML theme can still fail at runtime.
						// Require an actual test-mode preview and explicit user approval
						// before making it the boot-time greeter.
						plan.themeNeedsCheck = true
						plan.themeWarnings = append(plan.themeWarnings, fmt.Sprintf("SDDM theme %q must pass a graphical preview; if the preview is unavailable, crashes, or looks wrong, Zacman will keep %s active", themeName, sddmThemeLabel(fallback)))
					} else {
						plan.themeRecord = &sddmThemeVerification{Desired: cfg.Greeter.Theme, Fingerprint: fingerprint, Outcome: "approved", Fallback: fallback}
					}
				}
			}
			current := detectGreeterTheme("sddm")
			if current != plan.themeTarget || plan.themeNeedsCheck {
				label := sddmThemeLabel(plan.themeTarget)
				if plan.themeNeedsCheck && plan.themeTarget == themeName {
					plan.themeChanges = append(plan.themeChanges, fmt.Sprintf("preview and verify SDDM theme %s before activating it (otherwise use %s)", label, sddmThemeLabel(fallback)))
				} else {
					plan.themeChanges = append(plan.themeChanges, fmt.Sprintf("set SDDM theme to %s", label))
				}
			}
		}
	}
	if cfg.Mouse.Theme != "" || cfg.Mouse.Size > 0 {
		currentTheme, currentSize := detectCurrentMouseSettings()
		if cfg.Mouse.Theme != "" {
			name, dir, resolveErr := config.ResolveCursorTheme(cfg.Mouse.Theme)
			if resolveErr != nil {
				plan.mouseIssues = append(plan.mouseIssues, resolveErr.Error())
			} else {
				plan.mouseTarget, plan.mouseDir = name, dir
				// The package transaction can create a repo-sourced theme. Defer its
				// directory validation until after Pacman has successfully installed it.
				deferValidation := cfg.Mouse.ThemeSource == "repo" && pacmanInstallPending
				if !deferValidation {
					if validateErr := validateCursorThemeDir(dir); validateErr != nil {
						plan.mouseIssues = append(plan.mouseIssues, fmt.Sprintf("mouse theme %q: %v", cfg.Mouse.Theme, validateErr))
					}
				}
				if deferValidation || len(plan.mouseIssues) == 0 {
					if currentTheme != name {
						plan.mouseChanges = append(plan.mouseChanges, fmt.Sprintf("set mouse cursor theme to %s", name))
					}
				}
			}
		}
		if cfg.Mouse.Size > 0 && cfg.Mouse.Size != currentSize {
			plan.mouseChanges = append(plan.mouseChanges, fmt.Sprintf("set mouse cursor size to %d px", cfg.Mouse.Size))
		}
	}
	sort.Slice(plan.copies, func(i, j int) bool { return plan.copies[i].target < plan.copies[j].target })
	sort.Strings(plan.copyIssues)
	sort.Strings(plan.localeChanges)
	sort.Strings(plan.themeChanges)
	sort.Strings(plan.mouseChanges)
	sort.Strings(plan.mouseIssues)
	sort.Strings(plan.mouseWarnings)
	sort.Strings(plan.themeIssues)
	sort.Strings(plan.themeWarnings)
	return plan
}

func configuredMappings(cfg config.Config) []string {
	var values []string
	for _, section := range []config.PackageSection{
		cfg.Desktop, cfg.Audio, cfg.Kernel, cfg.Fonts,
		cfg.Python.UV, cfg.Python.Pip3, cfg.Node.NPM, cfg.Rust.Cargo, cfg.Golang,
	} {
		values = append(values, section.Config...)
	}
	return config.SortedUnique(values)
}

func inspectMapping(configPath, mapping string) (configCopy, string) {
	parts := strings.Split(mapping, "=>")
	if len(parts) != 2 {
		return configCopy{}, "invalid config mapping " + mapping
	}
	source := filepath.Join(config.ConfigDirectory(configPath), filepath.Clean(parts[0]))
	if err := rejectSymlinkPath(source, true); err != nil {
		return configCopy{}, fmt.Sprintf("config source %s: %v", parts[0], err)
	}
	info, err := os.Lstat(source)
	if err != nil {
		return configCopy{}, fmt.Sprintf("config source %s is unavailable: %v", parts[0], err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return configCopy{}, fmt.Sprintf("config source %s must be a regular non-symlink file", parts[0])
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return configCopy{}, fmt.Sprintf("read config source %s: %v", parts[0], err)
	}
	target, owner, err := resolveMappingTarget(parts[1])
	if err != nil {
		return configCopy{}, fmt.Sprintf("config destination %s: %v", parts[1], err)
	}
	if err := rejectSymlinkPath(target, false); err != nil {
		return configCopy{}, fmt.Sprintf("config destination %s: %v", parts[1], err)
	}
	item := configCopy{source: source, target: target, display: parts[1], data: data, mode: info.Mode().Perm() & 0755, owner: owner}
	if item.mode == 0 {
		item.mode = 0644
	}
	old, readErr := os.ReadFile(target)
	if readErr == nil {
		targetInfo, statErr := os.Lstat(target)
		if statErr != nil {
			return configCopy{}, fmt.Sprintf("inspect config destination %s: %v", parts[1], statErr)
		}
		if !targetInfo.Mode().IsRegular() || targetInfo.Mode()&os.ModeSymlink != 0 {
			return configCopy{}, fmt.Sprintf("config destination %s must be a regular non-symlink file", parts[1])
		}
		item.needsCopy = !bytes.Equal(old, data)
	} else if errors.Is(readErr, os.ErrNotExist) {
		item.needsCopy = true
	} else {
		return configCopy{}, fmt.Sprintf("read config destination %s: %v", parts[1], readErr)
	}
	return item, ""
}

func resolveMappingTarget(raw string) (string, *osuser.User, error) {
	if strings.HasPrefix(raw, "~/.config/") {
		account, err := targetUser()
		if err != nil {
			return "", nil, err
		}
		return filepath.Join(account.HomeDir, strings.TrimPrefix(raw, "~/")), account, nil
	}
	if !strings.HasPrefix(raw, "/etc/") {
		return "", nil, fmt.Errorf("destination must be under /etc/ or ~/.config/")
	}
	return filepath.Clean(raw), nil, nil
}

func targetUser() (*osuser.User, error) {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" && sudoUser != "root" {
		return osuser.Lookup(sudoUser)
	}
	if account, err := osuser.Current(); err == nil {
		return account, nil
	}
	return nil, fmt.Errorf("cannot determine destination home directory")
}

// rejectSymlinkPath checks every existing component with lstat so config copy
// operations never follow a symlink in the source or destination path.
func rejectSymlinkPath(path string, requireExisting bool) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for current := filepath.Clean(absolute); ; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing to follow symlink component %s", current)
			}
			if current != absolute && !info.IsDir() {
				return fmt.Errorf("path component %s is not a directory", current)
			}
			if current == absolute {
				break
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		} else if current == absolute && requireExisting {
			return statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

func printSystemConfigPlan(plan systemConfigPlan) {
	if len(plan.localeChanges) > 0 {
		fmt.Fprintln(os.Stdout, paint("cyan", "Locale and keyboard settings:"))
		for _, change := range plan.localeChanges {
			fmt.Fprintf(os.Stdout, "  %s %s\n", paint("mint", "+"), change)
		}
	}
	if len(plan.copies) > 0 {
		fmt.Fprintln(os.Stdout, paint("cyan", "Configuration files to copy:"))
		for _, item := range plan.copies {
			fmt.Fprintf(os.Stdout, "  %s %s -> %s\n", paint("mint", "+"), item.source, item.display)
		}
	}
	for _, issue := range plan.issues() {
		fmt.Fprintf(os.Stdout, "%s %s\n", paint("yellow", "Warning:"), issue)
	}
	for _, warning := range plan.themeWarnings {
		fmt.Fprintf(os.Stdout, "%s %s\n", paint("yellow", "Warning:"), warning)
	}
	for _, change := range plan.themeChanges {
		fmt.Fprintf(os.Stdout, "%s %s\n", paint("mint", "+"), change)
	}
	if len(plan.mouseChanges) > 0 {
		fmt.Fprintln(os.Stdout, paint("cyan", "Mouse cursor settings:"))
		for _, change := range plan.mouseChanges {
			fmt.Fprintf(os.Stdout, "  %s %s\n", paint("mint", "+"), change)
		}
	}
	for _, issue := range plan.mouseIssues {
		fmt.Fprintf(os.Stdout, "%s %s\n", paint("red", "Error:"), issue)
	}
	for _, warning := range plan.mouseWarnings {
		fmt.Fprintf(os.Stdout, "%s %s\n", paint("yellow", "Warning:"), warning)
	}
}

func localeDiff(desired config.LocaleConfig) []string {
	current := detectLocale()
	changes := []string{}
	check := func(label, want, have string) {
		if want != "" && want != have {
			changes = append(changes, fmt.Sprintf("set %s to %s", label, want))
		}
	}
	check("system language", desired.SystemLanguage, current.SystemLanguage)
	check("keyboard layout", desired.KeyboardLayout, current.KeyboardLayout)
	check("keyboard variant", desired.KeyboardVariant, current.KeyboardVariant)
	check("keyboard model", desired.KeyboardModel, current.KeyboardModel)
	check("keyboard options", desired.KeyboardOptions, current.KeyboardOptions)
	return changes
}

func applySystemConfig(cfg config.Config, configPath string, expected systemConfigPlan) error {
	if len(expected.issues()) > 0 {
		return errors.New("system configuration has unresolved issues; fix them before applying")
	}
	// Recalculate paths and contents right before writing to catch edited files
	// and symlink changes since the diff was displayed.
	fresh := buildSystemConfigPlan(cfg, configPath, false)
	if len(fresh.issues()) > 0 {
		return errors.New("system configuration changed or became unsafe; rerun 'zacman diff'")
	}
	for _, item := range fresh.copies {
		if err := atomicConfigCopy(item); err != nil {
			return fmt.Errorf("copy config %s -> %s: %w", item.source, item.display, err)
		}
		fmt.Fprintf(os.Stdout, "%s Copied config to %s\n", paint("mint", "✓"), item.display)
	}
	if err := applyLocale(cfg.Locale); err != nil {
		return err
	}
	if cfg.Greeter.Theme != "" && cfg.Greeter.System == "sddm" {
		target := fresh.themeTarget
		record := fresh.themeRecord
		if fresh.themeNeedsCheck && target != "" {
			themeName, themeDir, resolveErr := config.ResolveSDDMTheme(cfg.Greeter.Theme)
			fingerprint := sddmThemeFingerprint(themeDir)
			accepted, previewErr := previewAndConfirmSDDMTheme(themeDir)
			postPreviewFingerprint := sddmThemeFingerprint(themeDir)
			if accepted && postPreviewFingerprint != fingerprint {
				accepted = false
				previewErr = errors.New("theme files changed during preview; refusing to activate an unverified version")
				fingerprint = postPreviewFingerprint
			}
			if accepted && previewErr == nil && resolveErr == nil {
				target, record = themeName, &sddmThemeVerification{
					Desired: cfg.Greeter.Theme, Fingerprint: fingerprint, Outcome: "approved", Fallback: safeSDDMFallbackTheme(),
				}
				fmt.Fprintf(os.Stdout, "%s SDDM theme preview approved; activating %s.\n", paint("mint", "✓"), target)
			} else {
				fallback := safeSDDMFallbackTheme()
				if resolveErr != nil {
					previewErr = resolveErr
				}
				reason := "preview was not approved"
				if previewErr != nil {
					reason = previewErr.Error()
				}
				target = fallback
				record = &sddmThemeVerification{Desired: cfg.Greeter.Theme, Fingerprint: fingerprint, Outcome: "fallback", Fallback: fallback, Reason: reason}
				fmt.Fprintf(os.Stdout, "%s %s; keeping %s to prevent a broken login screen.\n", paint("yellow", "Warning:"), reason, sddmThemeLabel(fallback))
			}
		}
		if err := applySDDMThemeWithRecord(target, record); err != nil {
			return err
		}
	}
	if cfg.Mouse.Theme != "" || cfg.Mouse.Size > 0 {
		if err := applyMouseCursor(cfg.Mouse); err != nil {
			return err
		}
	}
	return nil
}

func atomicConfigCopy(item configCopy) error {
	if err := rejectSymlinkPath(filepath.Dir(item.target), false); err != nil {
		return err
	}
	if err := ensureDestinationDirectory(filepath.Dir(item.target), item.owner); err != nil {
		return err
	}
	if err := rejectSymlinkPath(filepath.Dir(item.target), false); err != nil {
		return err
	}
	if existing, err := os.Lstat(item.target); err == nil {
		if existing.Mode()&os.ModeSymlink != 0 || !existing.Mode().IsRegular() {
			return fmt.Errorf("destination is not a regular non-symlink file")
		}
		backup := item.target + ".zacman-backup"
		if _, err := os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
			old, err := os.ReadFile(item.target)
			if err != nil {
				return err
			}
			if err := writeNewFile(backup, old, existing.Mode().Perm()); err != nil {
				return fmt.Errorf("create backup %s: %w", backup, err)
			}
		} else if err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(item.target), ".zacman-config-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(item.mode); err != nil {
		return err
	}
	if _, err := tmp.Write(item.data); err != nil {
		return err
	}
	if item.owner != nil && os.Geteuid() == 0 {
		uid, _ := strconv.Atoi(item.owner.Uid)
		gid, _ := strconv.Atoi(item.owner.Gid)
		if err := tmp.Chown(uid, gid); err != nil {
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := rejectSymlinkPath(filepath.Dir(item.target), true); err != nil {
		return err
	}
	if err := os.Rename(tmpName, item.target); err != nil {
		return err
	}
	ok = true
	if dir, err := os.Open(filepath.Dir(item.target)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// ensureDestinationDirectory creates only missing path components. For targets
// under ~/.config when Zacman is invoked with sudo, newly created directories
// are owned by the invoking user rather than root. Existing directories are
// never chowned, and every existing component is checked for symlinks.
func ensureDestinationDirectory(path string, owner *osuser.User) error {
	path = filepath.Clean(path)
	if err := rejectSymlinkPath(path, false); err != nil {
		return err
	}
	missing := []string{}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refusing to follow symlink directory %s", current)
			}
			if !info.IsDir() {
				return fmt.Errorf("destination component %s is not a directory", current)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("no existing ancestor for destination directory %s", path)
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		dir := missing[i]
		created := true
		if err := os.Mkdir(dir, 0755); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("create destination directory %s: %w", dir, err)
			}
			created = false
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("destination component %s changed to a non-directory or symlink", dir)
		}
		if created && owner != nil && os.Geteuid() == 0 {
			uid, err := strconv.Atoi(owner.Uid)
			if err != nil {
				return fmt.Errorf("invalid uid for %s: %w", owner.Username, err)
			}
			gid, err := strconv.Atoi(owner.Gid)
			if err != nil {
				return fmt.Errorf("invalid gid for %s: %w", owner.Username, err)
			}
			if err := os.Chown(dir, uid, gid); err != nil {
				return fmt.Errorf("set owner on destination directory %s: %w", dir, err)
			}
		}
	}
	return nil
}

func writeNewFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func applyLocale(desired config.LocaleConfig) error {
	changes := localeDiff(desired)
	if len(changes) == 0 {
		return nil
	}
	if desired.SystemLanguage != "" && desired.SystemLanguage != "C" && desired.SystemLanguage != "POSIX" {
		if err := ensureLocaleGenerated(desired.SystemLanguage); err != nil {
			return err
		}
	}
	if desired.SystemLanguage != "" && containsPrefix(changes, "set system language to ") {
		if err := runCommand("localectl", "set-locale", "LANG="+desired.SystemLanguage); err != nil {
			return fmt.Errorf("set system language: %w", err)
		}
	}
	if desired.KeyboardLayout != "" && hasKeyboardChanges(changes) {
		args := []string{"set-x11-keymap", desired.KeyboardLayout}
		if desired.KeyboardModel != "" || desired.KeyboardVariant != "" || desired.KeyboardOptions != "" {
			args = append(args, desired.KeyboardModel, desired.KeyboardVariant, desired.KeyboardOptions)
		}
		if err := runCommand("localectl", args...); err != nil {
			return fmt.Errorf("set keyboard layout: %w", err)
		}
	}
	return nil
}

func ensureLocaleGenerated(language string) error {
	localeID := language
	if i := strings.IndexByte(localeID, '.'); i >= 0 {
		localeID = localeID[:i]
	}
	if i := strings.IndexByte(localeID, '@'); i >= 0 {
		localeID = localeID[:i]
	}
	path := "/etc/locale.gen"
	if err := rejectSymlinkPath(path, true); err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if !isRegularNonSymlink(path) {
		return fmt.Errorf("refusing to modify non-regular locale file %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	lines := strings.Split(string(data), "\n")
	want := localeID + ".UTF-8 UTF-8"
	found := false
	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 && fields[0] == localeID+".UTF-8" && fields[1] == "UTF-8" {
			found = true
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				prefix := ""
				if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
					prefix = line[:len(line)-len(strings.TrimLeft(line, " \t"))]
				}
				lines[i] = prefix + want
				changed = true
			}
			break
		}
	}
	if !found {
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, want)
		changed = true
	}
	if changed {
		backup := path + ".zacman-backup"
		if _, err := os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
			if err := writeNewFile(backup, data, 0644); err != nil {
				return fmt.Errorf("backup %s: %w", path, err)
			}
		} else if err != nil {
			return err
		}
		if err := atomicReplace(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
			return err
		}
		if err := runCommand("locale-gen"); err != nil {
			return fmt.Errorf("enable locale in %s, but locale-gen failed: %w", path, err)
		}
	}
	return nil
}

func atomicReplace(path string, data []byte, mode os.FileMode) error {
	if err := rejectSymlinkPath(filepath.Dir(path), true); err != nil {
		return err
	}
	if err := rejectSymlinkPath(path, false); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".zacman-system-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

const sddmThemeConfigPath = "/etc/sddm.conf.d/90-zacman-theme.conf"
const sddmThemeVerificationPrefix = "# Zacman theme verification: "

type sddmThemeVerification struct {
	Desired     string `json:"desired"`
	Fingerprint string `json:"fingerprint"`
	Outcome     string `json:"outcome"` // approved or fallback
	Fallback    string `json:"fallback"`
	Reason      string `json:"reason,omitempty"`
}

// validateSDDMThemeDir checks the theme's metadata and entry QML script before
// it can be made the boot-time greeter. Structural checks are complemented by
// a user-visible sddm-greeter test-mode preview below.
func validateSDDMThemeDir(themeDir string) error {
	if err := rejectSymlinkPath(themeDir, true); err != nil {
		return fmt.Errorf("theme directory path is unsafe: %w", err)
	}
	info, err := os.Lstat(themeDir)
	if err != nil {
		return fmt.Errorf("theme directory %s is unavailable: %w", themeDir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("theme path %s must be a real directory, not a symlink", themeDir)
	}
	metadataPath := filepath.Join(themeDir, "metadata.desktop")
	metadataInfo, err := os.Lstat(metadataPath)
	if err != nil {
		return fmt.Errorf("theme metadata.desktop is missing: %w", err)
	}
	if metadataInfo.Mode()&os.ModeSymlink != 0 || !metadataInfo.Mode().IsRegular() {
		return errors.New("theme metadata.desktop must be a regular file")
	}
	metadata, err := os.ReadFile(metadataPath)
	if err != nil {
		return fmt.Errorf("read theme metadata.desktop: %w", err)
	}
	if len(metadata) == 0 {
		return errors.New("theme metadata.desktop is empty")
	}
	if !strings.Contains(string(metadata), "[SddmGreeterTheme]") {
		return errors.New("theme metadata.desktop is missing the [SddmGreeterTheme] section")
	}
	mainScript := "Main.qml"
	scanner := bufio.NewScanner(bytes.NewReader(metadata))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		eq := strings.IndexByte(line, '=')
		if eq > 0 && strings.TrimSpace(line[:eq]) == "MainScript" {
			mainScript = strings.TrimSpace(line[eq+1:])
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read theme metadata.desktop: %w", err)
	}
	if mainScript == "" || filepath.IsAbs(mainScript) || strings.ContainsAny(mainScript, "\\\x00\r\n") {
		return fmt.Errorf("theme MainScript %q is invalid", mainScript)
	}
	cleanScript := filepath.Clean(mainScript)
	rel, err := filepath.Rel(filepath.Clean(themeDir), filepath.Join(filepath.Clean(themeDir), cleanScript))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("theme MainScript %q escapes the theme directory", mainScript)
	}
	scriptPath := filepath.Join(themeDir, cleanScript)
	if err := rejectSymlinkPath(scriptPath, true); err != nil {
		return fmt.Errorf("theme entry script path is unsafe: %w", err)
	}
	scriptInfo, err := os.Lstat(scriptPath)
	if err != nil {
		return fmt.Errorf("theme entry script %s is missing: %w", cleanScript, err)
	}
	if scriptInfo.Mode()&os.ModeSymlink != 0 || !scriptInfo.Mode().IsRegular() {
		return fmt.Errorf("theme entry script %s must be a regular file", cleanScript)
	}
	if scriptInfo.Size() == 0 {
		return fmt.Errorf("theme entry script %s is empty", cleanScript)
	}
	return nil
}

// safeSDDMFallbackTheme prefers Breeze when it is structurally installed. An
// empty Current selects SDDM's built-in embedded fallback if Breeze is absent.
func safeSDDMFallbackTheme() string {
	return fallbackSDDMThemeUnder("/usr/share/sddm/themes")
}

func fallbackSDDMThemeUnder(themesRoot string) string {
	if validateSDDMThemeDir(filepath.Join(themesRoot, "breeze")) == nil {
		return "breeze"
	}
	return ""
}

func sddmThemeLabel(theme string) string {
	if theme == "" {
		return "SDDM's embedded fallback theme"
	}
	return "theme " + theme
}

func sddmThemeFingerprint(themeDir string) string {
	h := sha256.New()
	rootInfo, err := os.Lstat(themeDir)
	if err != nil {
		_, _ = fmt.Fprintf(h, "missing:%s:%v", themeDir, err)
		return hex.EncodeToString(h.Sum(nil))
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		_, _ = fmt.Fprintf(h, "invalid:%s:%s", themeDir, rootInfo.Mode())
		return hex.EncodeToString(h.Sum(nil))
	}
	_ = filepath.WalkDir(themeDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			_, _ = fmt.Fprintf(h, "walk-error:%s:%v\n", path, walkErr)
			return nil
		}
		rel, relErr := filepath.Rel(themeDir, path)
		if relErr != nil {
			_, _ = fmt.Fprintf(h, "rel-error:%v\n", relErr)
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			_, _ = fmt.Fprintf(h, "info-error:%s:%v\n", rel, infoErr)
			return nil
		}
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%d\x00%d\x00%d\n", rel, info.Mode().String(), info.Size(), info.ModTime().UnixNano(), info.Mode().Type())
		if entry.Type()&os.ModeSymlink != 0 {
			if link, linkErr := os.Readlink(path); linkErr == nil {
				_, _ = fmt.Fprintf(h, "link:%s\x00%s\n", rel, link)
			}
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > 8*1024*1024 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".qml", ".js", ".desktop", ".conf", ".json":
			data, readErr := os.ReadFile(path)
			if readErr == nil {
				_, _ = h.Write(data)
			}
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

func readSDDMThemeVerification() (sddmThemeVerification, bool) {
	data, err := os.ReadFile(sddmThemeConfigPath)
	if err != nil {
		return sddmThemeVerification{}, false
	}
	return parseSDDMThemeVerification(data)
}

func parseSDDMThemeVerification(data []byte) (sddmThemeVerification, bool) {
	if !bytes.Contains(data, []byte("# Managed by Zacman.")) {
		return sddmThemeVerification{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, sddmThemeVerificationPrefix) {
			var record sddmThemeVerification
			if json.Unmarshal([]byte(strings.TrimPrefix(line, sddmThemeVerificationPrefix)), &record) == nil && record.Desired != "" && (record.Outcome == "approved" || record.Outcome == "fallback") {
				return record, true
			}
		}
	}
	return sddmThemeVerification{}, false
}

func previewAndConfirmSDDMTheme(themeDir string) (bool, error) {
	if err := validateSDDMThemeDir(themeDir); err != nil {
		return false, err
	}
	if info, err := os.Stdin.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false, errors.New("no interactive terminal is available to confirm the theme preview")
	}
	greeterPath := ""
	for _, candidate := range []string{"sddm-greeter-qt6", "sddm-greeter"} {
		if found, err := exec.LookPath(candidate); err == nil {
			greeterPath = found
			break
		}
	}
	if greeterPath == "" {
		return false, errors.New("sddm-greeter test executable was not found")
	}

	cmd, err := sddmPreviewCommand(greeterPath, themeDir)
	if err != nil {
		return false, err
	}
	fmt.Fprintf(os.Stdout, "\nOpening SDDM test preview for %s. Close the preview window to continue.\n", themeDir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("SDDM preview failed: %w", err)
	}
	fmt.Fprint(os.Stdout, "Did the theme render correctly in the preview? [y/N] ")
	var answer string
	if _, err := fmt.Fscanln(os.Stdin, &answer); err != nil {
		return false, fmt.Errorf("could not read preview confirmation: %w", err)
	}
	if strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes") {
		return true, nil
	}
	return false, errors.New("theme preview was not approved")
}

func sddmPreviewCommand(greeterPath, themeDir string) (*exec.Cmd, error) {
	vars := map[string]string{}
	for _, key := range []string{"DISPLAY", "XAUTHORITY", "WAYLAND_DISPLAY", "XDG_SESSION_TYPE", "QT_QPA_PLATFORM", "QT_QPA_PLATFORMTHEME", "XDG_CURRENT_DESKTOP", "XDG_SESSION_DESKTOP", "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR"} {
		if value := os.Getenv(key); value != "" {
			vars[key] = value
		}
	}

	if os.Geteuid() == 0 {
		username := strings.TrimSpace(os.Getenv("SUDO_USER"))
		if username == "" || username == "root" {
			return nil, errors.New("cannot safely preview a theme as root; run zacman through sudo from your normal desktop session")
		}
		account, err := osuser.Lookup(username)
		if err != nil {
			return nil, fmt.Errorf("look up desktop user %q: %w", username, err)
		}
		vars["HOME"], vars["USER"], vars["LOGNAME"] = account.HomeDir, account.Username, account.Username
		if vars["DISPLAY"] != "" && vars["XAUTHORITY"] == "" {
			xauthority := filepath.Join(account.HomeDir, ".Xauthority")
			if _, statErr := os.Stat(xauthority); statErr == nil {
				vars["XAUTHORITY"] = xauthority
			}
		}
		runtimeDir := filepath.Join("/run/user", account.Uid)
		if st, err := os.Stat(runtimeDir); err == nil && st.IsDir() {
			vars["XDG_RUNTIME_DIR"] = runtimeDir
		}
		if vars["WAYLAND_DISPLAY"] == "" && vars["XDG_RUNTIME_DIR"] != "" {
			if wayland := findWaylandDisplay(vars["XDG_RUNTIME_DIR"]); wayland != "" {
				vars["WAYLAND_DISPLAY"] = wayland
				vars["XDG_SESSION_TYPE"] = "wayland"
			}
		}
		if vars["DISPLAY"] == "" && vars["WAYLAND_DISPLAY"] == "" {
			return nil, errors.New("no active X11 or Wayland session was detected; retaining the safe fallback theme")
		}
		if vars["DBUS_SESSION_BUS_ADDRESS"] == "" && vars["XDG_RUNTIME_DIR"] != "" {
			bus := filepath.Join(vars["XDG_RUNTIME_DIR"], "bus")
			if _, err := os.Stat(bus); err == nil {
				vars["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=" + bus
			}
		}
		runuser, err := exec.LookPath("runuser")
		if err != nil {
			return nil, fmt.Errorf("runuser is required to preview the theme as %s: %w", username, err)
		}
		keys := make([]string, 0, len(vars))
		for key := range vars {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		args := []string{"-u", username, "--", "env"}
		for _, key := range keys {
			args = append(args, key+"="+vars[key])
		}
		args = append(args, greeterPath, "--test-mode", "--theme", themeDir)
		return exec.Command(runuser, args...), nil
	}

	if vars["DISPLAY"] == "" && vars["WAYLAND_DISPLAY"] == "" {
		return nil, errors.New("no active X11 or Wayland session was detected; retaining the safe fallback theme")
	}
	cmd := exec.Command(greeterPath, "--test-mode", "--theme", themeDir)
	cmd.Env = os.Environ()
	return cmd, nil
}

func findWaylandDisplay(runtimeDir string) string {
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "wayland-") {
			continue
		}
		info, err := os.Lstat(filepath.Join(runtimeDir, entry.Name()))
		if err == nil && info.Mode()&os.ModeSocket != 0 {
			return entry.Name()
		}
	}
	return ""
}

func applySDDMTheme(theme string) error {
	return applySDDMThemeWithRecord(theme, nil)
}

func applySDDMThemeWithRecord(theme string, record *sddmThemeVerification) error {
	current := ""
	if theme != "" {
		name, dir, err := config.ResolveSDDMTheme(theme)
		if err != nil {
			return err
		}
		if err := validateSDDMThemeDir(dir); err != nil {
			return fmt.Errorf("refusing to activate unsafe SDDM theme %q: %w", name, err)
		}
		current = name
	}
	data, err := renderSDDMThemeConfig(current, record)
	if err != nil {
		return err
	}
	path := sddmThemeConfigPath
	if err := rejectSymlinkPath(path, false); err != nil {
		return err
	}
	if existingInfo, statErr := os.Lstat(path); statErr == nil {
		if existingInfo.Mode()&os.ModeSymlink != 0 || !existingInfo.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular or symlink SDDM config %s", path)
		}
		existing, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !bytes.Contains(existing, []byte("# Managed by Zacman.")) {
			return fmt.Errorf("refusing to overwrite unmanaged SDDM config %s; move it aside or choose another config path", path)
		}
		if bytes.Equal(existing, data) {
			return nil
		}
		backup := path + ".zacman-backup"
		if _, backupErr := os.Lstat(backup); errors.Is(backupErr, os.ErrNotExist) {
			if err := writeNewFile(backup, existing, 0644); err != nil {
				return fmt.Errorf("save previous Zacman SDDM theme config: %w", err)
			}
		} else if backupErr != nil {
			return backupErr
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err := rejectSymlinkPath(filepath.Dir(path), false); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return atomicReplace(path, data, 0644)
}

func renderSDDMThemeConfig(current string, record *sddmThemeVerification) ([]byte, error) {
	if strings.ContainsAny(current, "\x00\r\n") {
		return nil, errors.New("invalid SDDM theme name")
	}
	contents := "# Managed by Zacman. Edit greeter.theme in /etc/zacman/zacman.toml instead.\n"
	if record != nil {
		metadata, err := json.Marshal(*record)
		if err != nil {
			return nil, err
		}
		contents += sddmThemeVerificationPrefix + string(metadata) + "\n"
	}
	contents += "[Theme]\nCurrent=" + current + "\n"
	return []byte(contents), nil
}

func hasKeyboardChanges(changes []string) bool {
	for _, change := range changes {
		if strings.HasPrefix(change, "set keyboard ") || strings.HasPrefix(change, "set keyboard layout ") {
			return true
		}
	}
	return false
}

func containsPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// Validate regular files without accidentally following an unexpected symlink.
func isRegularNonSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func hasPendingGreeterThemePackage(cfg config.Config, installing []string) bool {
	// This flag tells system configuration planning that a repository theme may
	// appear only after the Pacman transaction. It covers both SDDM themes and
	// cursor themes so pre-install validation does not reject an expected path.
	pending := makeSet(installing)
	for _, pkg := range cfg.Greeter.Pkgs {
		if pending[pkg] {
			return true
		}
	}
	if cfg.Greeter.ThemeSource == "repo" && cfg.Greeter.ThemePackage != "" && pending[cfg.Greeter.ThemePackage] {
		return true
	}
	for _, pkg := range cfg.Mouse.Pkgs {
		if pending[pkg] {
			return true
		}
	}
	return cfg.Mouse.ThemeSource == "repo" && cfg.Mouse.ThemePackage != "" && pending[cfg.Mouse.ThemePackage]
}
