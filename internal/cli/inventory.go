package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/T-Onix/zacman/internal/config"
	"github.com/T-Onix/zacman/internal/pkgmanager"
)

func inventoryPacmanPackages(repo, foreign []string, prior config.Inventory) config.Inventory {
	inv := prior
	inv.Repo = []string{}
	inv.Foreign = config.SortedUnique(foreign)
	inv.GreeterPkgs = []string{}
	inv.MousePkgs = []string{}
	inv.KernelPkgs = []string{}
	inv.AudioPkgs = []string{}
	inv.DesktopPkgs = []string{}
	inv.FontPkgs = []string{}
	foreignSet := makeSet(foreign)
	for _, pkg := range config.SortedUnique(repo) {
		if foreignSet[pkg] {
			continue
		}
		switch config.RecommendedPackageCategory(pkg) {
		case "greeter":
			inv.GreeterPkgs = append(inv.GreeterPkgs, pkg)
		case "mouse":
			inv.MousePkgs = append(inv.MousePkgs, pkg)
		case "kernel":
			inv.KernelPkgs = append(inv.KernelPkgs, pkg)
		case "audio":
			inv.AudioPkgs = append(inv.AudioPkgs, pkg)
		case "desktop":
			inv.DesktopPkgs = append(inv.DesktopPkgs, pkg)
		case "fonts":
			inv.FontPkgs = append(inv.FontPkgs, pkg)
		default:
			inv.Repo = append(inv.Repo, pkg)
		}
	}
	return inv
}

func collectManagerInventory(prior config.Inventory) (config.Inventory, []string, error) {
	inv := prior
	warnings := []string{}
	collect := func(label string, available bool, current []string, queryErr error, target *[]string) {
		if !available {
			return // keep the prior inventory when the optional manager is absent
		}
		if queryErr != nil {
			// A manager can be installed but unusable (missing remote, broken
			// Python environment, etc.). Preserve its previous inventory and
			// continue importing the rest of the system instead of aborting -axp.
			warnings = append(warnings, fmt.Sprintf("could not refresh %s inventory; preserved its previous entries: %v", label, queryErr))
			return
		}
		*target = config.SortedUnique(current)
	}

	if pkgmanager.Available(pkgmanager.Flatpak) {
		v, err := pkgmanager.Installed(pkgmanager.Flatpak, "system", "flathub")
		collect("system Flatpak", true, v, err, &inv.Flatpak)
	}
	if pkgmanager.Available(pkgmanager.Flatpak) {
		v, err := pkgmanager.Installed(pkgmanager.Flatpak, "user", "flathub")
		collect("user Flatpak", true, v, err, &inv.FlatpakUser)
	}
	for _, spec := range []struct {
		kind   pkgmanager.Kind
		name   string
		target *[]string
	}{
		{pkgmanager.UV, "uv tools", &inv.PythonUV},
		{pkgmanager.Pip3, "user pip3 packages", &inv.PythonPip3},
		{pkgmanager.NPM, "global npm packages", &inv.NodeNPM},
		{pkgmanager.Cargo, "Cargo-installed crates", &inv.RustCargo},
		{pkgmanager.Go, "Go-installed modules", &inv.Golang},
	} {
		if !pkgmanager.Available(spec.kind) {
			continue
		}
		current, err := pkgmanager.Installed(spec.kind, "user", "")
		collect(spec.name, true, current, err, spec.target)
	}
	locale := detectLocale()
	if locale.SystemLanguage != "" {
		inv.SystemLanguage = locale.SystemLanguage
	}
	if locale.KeyboardLayout != "" {
		inv.KeyboardLayout = locale.KeyboardLayout
	}
	if locale.KeyboardVariant != "" {
		inv.KeyboardVariant = locale.KeyboardVariant
	}
	if locale.KeyboardModel != "" {
		inv.KeyboardModel = locale.KeyboardModel
	}
	if locale.KeyboardOptions != "" {
		inv.KeyboardOptions = locale.KeyboardOptions
	}
	if greeter := detectGreeter(); greeter != "" {
		if inv.GreeterSystem != "" && greeter != inv.GreeterSystem {
			// A detected greeter change invalidates the previously observed SDDM
			// theme metadata. It must not make a non-SDDM inventory fail validation.
			inv.GreeterTheme = ""
			inv.GreeterThemeSource = ""
			inv.GreeterThemePackage = ""
		}
		inv.GreeterSystem = greeter
	}
	if inv.GreeterSystem == "sddm" {
		if theme := detectGreeterTheme(inv.GreeterSystem); theme != "" {
			inv.GreeterTheme = theme
			inv.GreeterThemeSource = "local"
			inv.GreeterThemePackage = ""
		}
	} else {
		inv.GreeterTheme = ""
		inv.GreeterThemeSource = ""
		inv.GreeterThemePackage = ""
	}
	if inv.GreeterThemeSource == "" {
		inv.GreeterThemeSource = "local"
	}
	if theme, size := detectCurrentMouseSettings(); theme != "" {
		inv.MouseTheme = theme
		inv.MouseThemeSource = "local"
		inv.MouseSize = size
	}
	return inv, warnings, nil
}

func detectLocale() config.LocaleConfig {
	value := config.LocaleConfig{}
	out, err := exec.Command("localectl", "status").Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			trimmed := strings.TrimSpace(line)
			lower := strings.ToLower(trimmed)
			switch {
			case strings.HasPrefix(lower, "system locale:"):
				if i := strings.Index(trimmed, "LANG="); i >= 0 {
					fields := strings.Fields(trimmed[i+5:])
					if len(fields) > 0 {
						value.SystemLanguage = cleanLocaleValue(fields[0])
					}
				}
			case strings.HasPrefix(lower, "x11 layout:"):
				value.KeyboardLayout = cleanLocaleValue(trimmed[strings.Index(trimmed, ":")+1:])
			case strings.HasPrefix(lower, "x11 model:"):
				value.KeyboardModel = cleanLocaleValue(trimmed[strings.Index(trimmed, ":")+1:])
			case strings.HasPrefix(lower, "x11 variant:"):
				value.KeyboardVariant = cleanLocaleValue(trimmed[strings.Index(trimmed, ":")+1:])
			case strings.HasPrefix(lower, "x11 options:"):
				value.KeyboardOptions = cleanLocaleValue(trimmed[strings.Index(trimmed, ":")+1:])
			}
		}
	}
	if value.SystemLanguage == "" {
		if b, err := os.ReadFile("/etc/locale.conf"); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "LANG=") {
					value.SystemLanguage = cleanLocaleValue(strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "LANG=")), `"'`))
				}
			}
		}
	}
	return value
}

func cleanLocaleValue(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "n/a", "unset", "(unset)", "none", "(none)":
		return ""
	default:
		return value
	}
}

func detectGreeter() string {
	out, err := exec.Command("systemctl", "show", "--property=FragmentPath", "--value", "display-manager.service").Output()
	if err != nil {
		return ""
	}
	base := filepath.Base(strings.TrimSpace(string(out)))
	base = strings.TrimSuffix(base, ".service")
	for _, candidate := range []string{"sddm", "gdm", "lightdm", "greetd", "ly", "emptty"} {
		if base == candidate {
			return candidate
		}
	}
	return ""
}

func detectGreeterTheme(greeter string) string {
	if greeter != "sddm" {
		return ""
	}
	files := []string{"/etc/sddm.conf"}
	if entries, err := os.ReadDir("/etc/sddm.conf.d"); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".conf") {
				files = append(files, filepath.Join("/etc/sddm.conf.d", entry.Name()))
			}
		}
	}
	sort.Strings(files)
	var found string
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		section := ""
		scanner := bufio.NewScanner(strings.NewReader(string(b)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				section = strings.ToLower(strings.Trim(line, "[] "))
				continue
			}
			if section != "theme" {
				continue
			}
			if strings.HasPrefix(line, "Current=") {
				found = strings.TrimSpace(strings.TrimPrefix(line, "Current="))
			}
			if strings.HasPrefix(line, "Theme=") {
				found = strings.TrimSpace(strings.TrimPrefix(line, "Theme="))
			}
		}
	}
	if found != "" {
		if _, err := os.Stat(filepath.Join("/usr/share/sddm/themes", found)); err == nil {
			return found
		}
	}
	return ""
}
