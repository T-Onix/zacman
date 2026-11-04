package pacman

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultPacmanConf       = "/etc/pacman.conf"
	DefaultMirrorlist       = "/etc/pacman.d/mirrorlist"
	DefaultBlackarchMirrors = "/etc/pacman.d/zacman-blackarch-mirrorlist"
	candyBegin              = "# >>> zacman-managed-ilovecandy >>>"
	candyEnd                = "# <<< zacman-managed-ilovecandy <<<"
	mirrorBegin             = "# >>> zacman-managed-mirrors >>>"
	mirrorEnd               = "# <<< zacman-managed-mirrors <<<"
	blackarchMirrorBegin    = "# >>> zacman-managed-blackarch-mirrors >>>"
	blackarchMirrorEnd      = "# <<< zacman-managed-blackarch-mirrors <<<"
	blackarchIncludeBegin   = "# >>> zacman-managed-blackarch-include >>>"
	blackarchIncludeEnd     = "# <<< zacman-managed-blackarch-include <<<"
)

// HasBlackArchRepository reports whether /etc/pacman.conf has an active
// [blackarch] section. Commented examples such as #[blackarch] do not count.
func HasBlackArchRepository() bool {
	data, err := os.ReadFile(DefaultPacmanConf)
	return err == nil && HasBlackArchRepositoryIn(data)
}

// HasBlackArchRepositoryIn is the pure parser used by config planning and tests.
func HasBlackArchRepositoryIn(data []byte) bool {
	for _, line := range splitLinesPreserve(data) {
		trimmed := strings.TrimSpace(line)
		if isSectionHeader(trimmed) && strings.EqualFold(strings.TrimSpace(trimmed[1:len(trimmed)-1]), "blackarch") {
			return true
		}
	}
	return false
}

// SystemSettingsDiff lists only Pacman settings that would change, allowing
// the CLI to keep its plan concise without hiding managed configuration changes.
func SystemSettingsDiff(iloveCandy bool, mirrors, blackarchMirrors []string) ([]string, error) {
	changes := []string{}
	conf, err := os.ReadFile(DefaultPacmanConf)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", DefaultPacmanConf, err)
	}
	updatedConf, err := RenderILoveCandy(conf, iloveCandy)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(conf, updatedConf) {
		if iloveCandy {
			changes = append(changes, "enable Pacman ILoveCandy progress style")
		} else {
			changes = append(changes, "disable Pacman ILoveCandy progress style")
		}
	}
	if len(blackarchMirrors) > 0 && !HasBlackArchRepositoryIn(updatedConf) {
		changes = append(changes, "enable the BlackArch repository and keyring using the checksum-verified official bootstrap script")
	} else {
		blackarchConf, renderErr := RenderBlackarchInclude(updatedConf, len(blackarchMirrors) > 0)
		if renderErr != nil {
			return nil, renderErr
		}
		if !bytes.Equal(updatedConf, blackarchConf) {
			if len(blackarchMirrors) > 0 {
				changes = append(changes, "connect the configured BlackArch mirror list to the existing [blackarch] repository")
			} else {
				changes = append(changes, "remove Zacman's BlackArch mirror include")
			}
		}
	}

	if diff, err := mirrorDiff(DefaultMirrorlist, mirrors, mirrorBegin, mirrorEnd, "custom mirror"); err != nil {
		return nil, err
	} else if diff != "" {
		changes = append(changes, diff)
	}
	if diff, err := mirrorDiff(DefaultBlackarchMirrors, blackarchMirrors, blackarchMirrorBegin, blackarchMirrorEnd, "BlackArch mirror"); err != nil {
		return nil, err
	} else if diff != "" {
		changes = append(changes, diff)
	}
	return changes, nil
}

func mirrorDiff(path string, servers []string, begin, end, label string) (string, error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return "", fmt.Errorf("read %s: %w", path, readErr)
	}
	if errors.Is(readErr, os.ErrNotExist) && len(servers) == 0 {
		return "", nil
	}
	updated, err := RenderManagedMirrorlist(data, servers, begin, end)
	if err != nil {
		return "", err
	}
	if bytes.Equal(data, updated) {
		return "", nil
	}
	if len(servers) > 0 {
		return fmt.Sprintf("synchronize %d configured %s(s)", len(servers), label), nil
	}
	return fmt.Sprintf("remove Zacman-managed %s entries", label), nil
}

// SyncSystemSettings applies Zacman's managed Pacman options and extra mirrors
// while preserving unrelated user configuration. BlackArch mirror URLs are
// only connected when a [blackarch] repository section already exists.
func SyncSystemSettings(iloveCandy bool, mirrors, blackarchMirrors []string) error {
	if len(blackarchMirrors) > 0 && !HasBlackArchRepository() {
		return errors.New("BlackArch mirrors are configured but its repository/keyring is not enabled; run 'sudo zacman repo enable blackarch' or reconcile after configuring mirrors")
	}
	if _, err := SystemSettingsDiff(iloveCandy, mirrors, blackarchMirrors); err != nil {
		return err
	}
	if err := syncMirrorlist(DefaultMirrorlist, mirrors, mirrorBegin, mirrorEnd); err != nil {
		return fmt.Errorf("sync custom mirrors: %w", err)
	}
	if err := syncMirrorlist(DefaultBlackarchMirrors, blackarchMirrors, blackarchMirrorBegin, blackarchMirrorEnd); err != nil {
		return fmt.Errorf("sync BlackArch mirrors: %w", err)
	}
	conf, err := os.ReadFile(DefaultPacmanConf)
	if err != nil {
		return err
	}
	updated, err := RenderILoveCandy(conf, iloveCandy)
	if err != nil {
		return err
	}
	updated, err = RenderBlackarchInclude(updated, len(blackarchMirrors) > 0)
	if err != nil {
		return err
	}
	if !bytes.Equal(conf, updated) {
		if err := atomicReplace(DefaultPacmanConf, updated, 0644); err != nil {
			return fmt.Errorf("write %s: %w", DefaultPacmanConf, err)
		}
	}
	return nil
}

func syncMirrorlist(path string, servers []string, begin, end string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) && len(servers) == 0 {
		return nil
	}
	updated, err := RenderManagedMirrorlist(data, servers, begin, end)
	if err != nil {
		return err
	}
	if bytes.Equal(data, updated) {
		return nil
	}
	return atomicReplace(path, updated, 0644)
}

// RenderMirrorlist replaces Zacman's managed official-repository mirror block.
func RenderMirrorlist(original []byte, servers []string) ([]byte, error) {
	return RenderManagedMirrorlist(original, servers, mirrorBegin, mirrorEnd)
}

// RenderBlackarchMirrorlist replaces Zacman's managed BlackArch-only mirror block.
func RenderBlackarchMirrorlist(original []byte, servers []string) ([]byte, error) {
	return RenderManagedMirrorlist(original, servers, blackarchMirrorBegin, blackarchMirrorEnd)
}

// RenderManagedMirrorlist places configured mirror servers first and preserves
// all unowned lines. Marker blocks make repeated reconciliation idempotent.
func RenderManagedMirrorlist(original []byte, servers []string, begin, end string) ([]byte, error) {
	lines := splitLinesPreserve(original)
	cleaned, count, err := removeMarkerBlock(lines, begin, end)
	if err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		if count > 0 && len(cleaned) > 0 && cleaned[0] == "" {
			cleaned = cleaned[1:]
		}
		return joinLines(cleaned), nil
	}
	block := []string{begin}
	for _, server := range servers {
		block = append(block, "Server = "+server)
	}
	block = append(block, end)
	if len(cleaned) > 0 && cleaned[0] != "" {
		block = append(block, "")
	}
	return joinLines(append(block, cleaned...)), nil
}

// RenderILoveCandy updates the ILoveCandy directive inside [options]. The
// marker block makes repeat runs idempotent. If disabled, Zacman removes active
// ILoveCandy directives from [options] so the TOML remains authoritative.
func RenderILoveCandy(original []byte, enabled bool) ([]byte, error) {
	lines := splitLinesPreserve(original)
	cleaned, _, err := removeMarkerBlock(lines, candyBegin, candyEnd)
	if err != nil {
		return nil, err
	}
	inOptions := false
	optionsFound := false
	alreadyEnabled := false
	for _, line := range cleaned {
		trimmed := strings.TrimSpace(line)
		if isSectionHeader(trimmed) {
			inOptions = strings.EqualFold(strings.TrimSpace(trimmed[1:len(trimmed)-1]), "options")
			if inOptions {
				optionsFound = true
			}
			continue
		}
		if inOptions && activeConfigLine(trimmed) == "ILoveCandy" {
			alreadyEnabled = true
		}
	}
	if !optionsFound {
		return nil, errors.New("[options] section not found in /etc/pacman.conf")
	}
	if !enabled || alreadyEnabled {
		if !enabled {
			cleaned = removeOptionInOptions(cleaned, "ILoveCandy")
		}
		return joinLines(cleaned), nil
	}
	block := []string{candyBegin, "ILoveCandy", candyEnd}
	for i, line := range cleaned {
		if strings.EqualFold(strings.TrimSpace(line), "[options]") {
			out := make([]string, 0, len(cleaned)+len(block))
			out = append(out, cleaned[:i+1]...)
			out = append(out, block...)
			out = append(out, cleaned[i+1:]...)
			return joinLines(out), nil
		}
	}
	return nil, errors.New("[options] section not found in /etc/pacman.conf")
}

// RenderBlackarchInclude adds a managed Include inside an already-enabled
// [blackarch] repository section. It never enables a third-party repo itself.
func RenderBlackarchInclude(original []byte, enabled bool) ([]byte, error) {
	lines := splitLinesPreserve(original)
	cleaned, _, err := removeMarkerBlock(lines, blackarchIncludeBegin, blackarchIncludeEnd)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return joinLines(cleaned), nil
	}
	for i, line := range cleaned {
		trimmed := strings.TrimSpace(line)
		if isSectionHeader(trimmed) && strings.EqualFold(strings.TrimSpace(trimmed[1:len(trimmed)-1]), "blackarch") {
			block := []string{blackarchIncludeBegin, "Include = " + DefaultBlackarchMirrors, blackarchIncludeEnd}
			out := make([]string, 0, len(cleaned)+len(block))
			out = append(out, cleaned[:i+1]...)
			out = append(out, block...)
			out = append(out, cleaned[i+1:]...)
			return joinLines(out), nil
		}
	}
	return nil, errors.New("mirrors.blackarch_servers is configured but /etc/pacman.conf has no [blackarch] section; enable the BlackArch repository first")
}

func isSectionHeader(line string) bool {
	return strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, ";")
}

func removeOptionInOptions(lines []string, option string) []string {
	out := make([]string, 0, len(lines))
	inOptions := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isSectionHeader(trimmed) {
			inOptions = strings.EqualFold(strings.TrimSpace(trimmed[1:len(trimmed)-1]), "options")
		}
		if inOptions && activeConfigLine(trimmed) == option {
			continue
		}
		out = append(out, line)
	}
	return out
}

func activeConfigLine(line string) string {
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
		return ""
	}
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	return line
}

func removeMarkerBlock(lines []string, begin, end string) ([]string, int, error) {
	out := make([]string, 0, len(lines))
	inside := false
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case begin:
			if inside || count != 0 {
				return nil, count, fmt.Errorf("duplicate or nested managed block %q", begin)
			}
			inside = true
			count++
			continue
		case end:
			if !inside {
				return nil, count, fmt.Errorf("orphan managed block end %q", end)
			}
			inside = false
			continue
		}
		if !inside {
			out = append(out, line)
		}
	}
	if inside {
		return nil, count, fmt.Errorf("unterminated managed block %q", begin)
	}
	return out, count, nil
}

func splitLinesPreserve(data []byte) []string {
	if len(data) == 0 {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func joinLines(lines []string) []byte {
	if len(lines) == 0 {
		return []byte{}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func atomicReplace(path string, data []byte, defaultMode os.FileMode) error {
	parent := filepath.Dir(path)
	if pinfo, err := os.Lstat(parent); err != nil {
		return fmt.Errorf("inspect parent directory %s: %w", parent, err)
	} else if pinfo.Mode()&os.ModeSymlink != 0 || !pinfo.IsDir() {
		return fmt.Errorf("refusing unsafe parent directory %s", parent)
	}
	mode := defaultMode
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular or symlink path %s", path)
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(parent, ".zacman-system-config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
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
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	if dir, err := os.Open(parent); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}
