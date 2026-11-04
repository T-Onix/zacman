package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	osuser "os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/T-Onix/zacman/internal/config"
)

// validateCursorThemeDir checks the filesystem shape used by Xcursor themes.
// A valid theme must be a real directory containing a non-empty cursors folder.
// index.theme is optional in the Xcursor ecosystem, but when present it must be
// a regular file (not a symlink or special file).
func validateCursorThemeDir(themeDir string) error {
	if err := rejectSymlinkPath(themeDir, true); err != nil {
		return fmt.Errorf("cursor theme path is unsafe: %w", err)
	}
	info, err := os.Lstat(themeDir)
	if err != nil {
		return fmt.Errorf("cursor theme directory %s is unavailable: %w", themeDir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("cursor theme path %s must be a real directory, not a symlink", themeDir)
	}
	cursorDir := filepath.Join(themeDir, "cursors")
	cursorInfo, err := os.Lstat(cursorDir)
	if err != nil {
		return fmt.Errorf("cursor theme %s has no cursors directory: %w", themeDir, err)
	}
	if cursorInfo.Mode()&os.ModeSymlink != 0 || !cursorInfo.IsDir() {
		return fmt.Errorf("cursor theme %s must contain a real cursors directory", themeDir)
	}
	entries, err := os.ReadDir(cursorDir)
	if err != nil {
		return fmt.Errorf("read cursor assets: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("cursor theme %s has an empty cursors directory", themeDir)
	}
	metadata := filepath.Join(themeDir, "index.theme")
	if meta, statErr := os.Lstat(metadata); statErr == nil {
		if meta.Mode()&os.ModeSymlink != 0 || !meta.Mode().IsRegular() {
			return fmt.Errorf("cursor theme index.theme must be a regular file")
		}
		b, readErr := os.ReadFile(metadata)
		if readErr != nil {
			return fmt.Errorf("read cursor theme index.theme: %w", readErr)
		}
		if len(bytes.TrimSpace(b)) == 0 {
			return errors.New("cursor theme index.theme is empty")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	return nil
}

// detectCurrentMouseSettings reads the invoking desktop user's persistent KDE
// settings first, then the Xcursor default-theme fallback and GTK settings.
func detectCurrentMouseSettings() (string, int) {
	account, err := targetUser()
	if err != nil || account.HomeDir == "" {
		return "", 0
	}
	home := account.HomeDir
	theme := readINIValue(filepath.Join(home, ".config", "kcminputrc"), "Mouse", "cursorTheme")
	size := parsePositiveInt(readINIValue(filepath.Join(home, ".config", "kcminputrc"), "Mouse", "cursorSize"))
	if theme == "" {
		theme = readINIValue(filepath.Join(home, ".local", "share", "icons", "default", "index.theme"), "Icon Theme", "Inherits")
	}
	if theme == "" {
		theme = readINIValue(filepath.Join(home, ".icons", "default", "index.theme"), "Icon Theme", "Inherits")
	}
	if theme == "" {
		gtk3 := filepath.Join(home, ".config", "gtk-3.0", "settings.ini")
		theme = readINIValue(gtk3, "Settings", "gtk-cursor-theme-name")
	}
	if theme == "" {
		theme = readINIValue(filepath.Join(home, ".config", "gtk-4.0", "settings.ini"), "Settings", "gtk-cursor-theme-name")
	}
	if size == 0 {
		size = parsePositiveInt(readINIValue(filepath.Join(home, ".config", "gtk-3.0", "settings.ini"), "Settings", "gtk-cursor-theme-size"))
	}
	if size == 0 {
		size = parsePositiveInt(readINIValue(filepath.Join(home, ".config", "gtk-4.0", "settings.ini"), "Settings", "gtk-cursor-theme-size"))
	}
	if theme == "" {
		theme = readINIValue(filepath.Join(home, ".config", "environment.d", "90-zacman-cursor.conf"), "", "XCURSOR_THEME")
	}
	if size == 0 {
		size = parsePositiveInt(readINIValue(filepath.Join(home, ".config", "environment.d", "90-zacman-cursor.conf"), "", "XCURSOR_SIZE"))
	}
	return strings.TrimSpace(theme), size
}

func applyMouseCursor(mouse config.MouseConfig) error {
	if mouse.Theme == "" && mouse.Size == 0 {
		return nil
	}
	account, err := targetUser()
	if err != nil {
		return fmt.Errorf("resolve desktop user for mouse cursor configuration: %w", err)
	}
	if os.Geteuid() == 0 && (strings.TrimSpace(os.Getenv("SUDO_USER")) == "" || os.Getenv("SUDO_USER") == "root") {
		return errors.New("refusing to change root's cursor settings; run zacman through sudo from your normal desktop user session")
	}
	if account.HomeDir == "" {
		return errors.New("desktop user's home directory is unavailable")
	}

	themeName := ""
	if mouse.Theme != "" {
		name, dir, resolveErr := config.ResolveCursorTheme(mouse.Theme)
		if resolveErr != nil {
			return fmt.Errorf("resolve mouse theme: %w", resolveErr)
		}
		if err := validateCursorThemeDir(dir); err != nil {
			return fmt.Errorf("refusing to apply cursor theme %q: %w", mouse.Theme, err)
		}
		themeName = name
	}
	home := account.HomeDir
	if themeName != "" {
		// KDE/Plasma is the system-of-record on the target user's account.
		if err := updateUserINI(filepath.Join(home, ".config", "kcminputrc"), "Mouse", "cursorTheme", themeName, account); err != nil {
			return fmt.Errorf("update KDE cursor theme: %w", err)
		}
		// A default Xcursor theme makes Plasma Wayland and toolkits that consult
		// the freedesktop icon fallback agree on the selected cursor theme.
		if err := updateUserINI(filepath.Join(home, ".local", "share", "icons", "default", "index.theme"), "Icon Theme", "Inherits", themeName, account); err != nil {
			return fmt.Errorf("update default Xcursor theme: %w", err)
		}
	}
	if mouse.Size > 0 {
		if err := updateUserINI(filepath.Join(home, ".config", "kcminputrc"), "Mouse", "cursorSize", strconv.Itoa(mouse.Size), account); err != nil {
			return fmt.Errorf("update KDE cursor size: %w", err)
		}
	}
	// GTK 3 and GTK 4 applications keep their own settings; update only the
	// cursor keys while preserving every unrelated setting and comment.
	for _, version := range []string{"gtk-3.0", "gtk-4.0"} {
		path := filepath.Join(home, ".config", version, "settings.ini")
		if themeName != "" {
			if err := updateUserINI(path, "Settings", "gtk-cursor-theme-name", themeName, account); err != nil {
				return fmt.Errorf("update %s cursor theme: %w", version, err)
			}
		}
		if mouse.Size > 0 {
			if err := updateUserINI(path, "Settings", "gtk-cursor-theme-size", strconv.Itoa(mouse.Size), account); err != nil {
				return fmt.Errorf("update %s cursor size: %w", version, err)
			}
		}
	}
	gtk2 := filepath.Join(home, ".gtkrc-2.0")
	if themeName != "" {
		if err := updateUserPlainKey(gtk2, "gtk-cursor-theme-name", strconv.Quote(themeName), account); err != nil {
			return fmt.Errorf("update GTK 2 cursor theme: %w", err)
		}
	}
	if mouse.Size > 0 {
		if err := updateUserPlainKey(gtk2, "gtk-cursor-theme-size", strconv.Itoa(mouse.Size), account); err != nil {
			return fmt.Errorf("update GTK 2 cursor size: %w", err)
		}
	}
	// systemd's per-user environment imports these on the next login. This is
	// additive: existing unrelated variables and comments in this file survive.
	envPath := filepath.Join(home, ".config", "environment.d", "90-zacman-cursor.conf")
	if themeName != "" {
		if err := updateUserEnv(envPath, "XCURSOR_THEME", themeName, account); err != nil {
			return fmt.Errorf("update Xcursor environment: %w", err)
		}
	}
	if mouse.Size > 0 {
		if err := updateUserEnv(envPath, "XCURSOR_SIZE", strconv.Itoa(mouse.Size), account); err != nil {
			return fmt.Errorf("update Xcursor environment size: %w", err)
		}
	}
	fmt.Fprintf(os.Stdout, "%s Saved mouse cursor settings for %s. Log out and back in for all applications to pick them up.\n", paint("mint", "✓"), account.Username)
	return nil
}

func readINIValue(path, section, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	currentSection := ""
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if currentSection != section {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 1 || strings.TrimSpace(line[:eq]) != key {
			continue
		}
		value := strings.TrimSpace(line[eq+1:])
		if idx := strings.IndexAny(value, "#;"); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
		return strings.Trim(value, `"'`)
	}
	return ""
}

func parsePositiveInt(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

func setINIValue(data []byte, section, key, value string) []byte {
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	sectionStart, sectionEnd := -1, len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			current := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			if sectionStart >= 0 {
				sectionEnd = i
				break
			}
			if current == section {
				sectionStart = i
			}
		}
	}
	if sectionStart < 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "["+section+"]", key+"="+value)
		return []byte(strings.Join(lines, "\n") + "\n")
	}
	for i := sectionStart + 1; i < sectionEnd; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		eq := strings.IndexByte(trimmed, '=')
		if eq < 1 || strings.TrimSpace(trimmed[:eq]) != key {
			continue
		}
		original := lines[i]
		originalEq := strings.IndexByte(original, '=')
		prefix := original[:originalEq+1]
		tail := strings.TrimLeft(original[originalEq+1:], " \t")
		comment := ""
		if idx := strings.IndexAny(tail, "#;"); idx >= 0 {
			comment = " " + strings.TrimSpace(tail[idx:])
		}
		lines[i] = prefix + value + comment
		return []byte(strings.Join(lines, "\n") + "\n")
	}
	insert := sectionEnd
	for insert > sectionStart+1 && strings.TrimSpace(lines[insert-1]) == "" {
		insert--
	}
	lines = append(lines, "")
	copy(lines[insert+1:], lines[insert:])
	lines[insert] = key + "=" + value
	return []byte(strings.Join(lines, "\n") + "\n")
}

func setPlainValue(data []byte, key, value string) []byte {
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		eq := strings.IndexByte(trimmed, '=')
		if eq < 1 || strings.TrimSpace(trimmed[:eq]) != key {
			continue
		}
		lines[i] = key + "=" + value
		return []byte(strings.Join(lines, "\n") + "\n")
	}
	lines = append(lines, key+"="+value)
	return []byte(strings.Join(lines, "\n") + "\n")
}

func setEnvironmentValue(data []byte, key, value string) []byte {
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if eq := strings.IndexByte(trimmed, '='); eq > 0 && strings.TrimSpace(trimmed[:eq]) == key {
			lines[i] = key + "=" + value
			return []byte(strings.Join(lines, "\n") + "\n")
		}
	}
	lines = append(lines, key+"="+value)
	return []byte(strings.Join(lines, "\n") + "\n")
}

func updateUserINI(path, section, key, value string, owner *osuser.User) error {
	return updateUserText(path, owner, func(data []byte) []byte { return setINIValue(data, section, key, value) })
}

func updateUserPlainKey(path, key, value string, owner *osuser.User) error {
	return updateUserText(path, owner, func(data []byte) []byte { return setPlainValue(data, key, value) })
}

func updateUserEnv(path, key, value string, owner *osuser.User) error {
	return updateUserText(path, owner, func(data []byte) []byte { return setEnvironmentValue(data, key, value) })
}

func updateUserText(path string, owner *osuser.User, transform func([]byte) []byte) error {
	path = filepath.Clean(path)
	if err := rejectSymlinkPath(filepath.Dir(path), false); err != nil {
		return err
	}
	if err := ensureDestinationDirectory(filepath.Dir(path), owner); err != nil {
		return err
	}
	if err := rejectSymlinkPath(filepath.Dir(path), true); err != nil {
		return err
	}
	var old []byte
	var priorInfo os.FileInfo
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to modify non-regular or symlink file %s", path)
		}
		priorInfo = info
		old, err = os.ReadFile(path)
		if err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	updated := transform(old)
	if bytes.Equal(old, updated) {
		return nil
	}
	if priorInfo != nil {
		backup := path + ".zacman-backup"
		if backupInfo, backupErr := os.Lstat(backup); backupErr == nil {
			if backupInfo.Mode()&os.ModeSymlink != 0 || !backupInfo.Mode().IsRegular() {
				return fmt.Errorf("refusing to use unsafe backup path %s", backup)
			}
		} else if errors.Is(backupErr, os.ErrNotExist) {
			if err := writeNewFile(backup, old, priorInfo.Mode().Perm()); err != nil {
				return fmt.Errorf("back up %s: %w", path, err)
			}
			if os.Geteuid() == 0 {
				uid, uidErr := strconv.Atoi(owner.Uid)
				gid, gidErr := strconv.Atoi(owner.Gid)
				if uidErr != nil || gidErr != nil {
					return fmt.Errorf("invalid UID/GID for desktop user %s", owner.Username)
				}
				if err := os.Chown(backup, uid, gid); err != nil {
					return fmt.Errorf("set backup ownership on %s: %w", backup, err)
				}
			}
		} else {
			return backupErr
		}
	}
	mode := os.FileMode(0644)
	if priorInfo != nil {
		mode = priorInfo.Mode().Perm() & 0666
		if mode == 0 {
			mode = 0644
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".zacman-cursor-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if os.Geteuid() == 0 {
		uid, uidErr := strconv.Atoi(owner.Uid)
		gid, gidErr := strconv.Atoi(owner.Gid)
		if uidErr != nil || gidErr != nil {
			tmp.Close()
			return fmt.Errorf("invalid UID/GID for desktop user %s", owner.Username)
		}
		if err := tmp.Chown(uid, gid); err != nil {
			tmp.Close()
			return err
		}
	}
	if _, err := tmp.Write(updated); err != nil {
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
	if err := rejectSymlinkPath(path, false); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
