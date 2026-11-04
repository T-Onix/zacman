package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	osuser "os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

const (
	DefaultPath   = "/etc/zacman/zacman.toml"
	SchemaVersion = 1
)

var (
	packageNameRE    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._+-]*$`)
	unitNameRE       = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9:_.@-]*\.(service|socket|target|timer|path|mount|automount|swap|slice)$`)
	managerPackageRE = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9@._+:/<>=!~^,-]*|@[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._+~^-]*(@[A-Za-z0-9._+~-]+)?)$`)
	themeNameRE      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	localeNameRE     = regexp.MustCompile(`^(C(\.UTF-8)?|POSIX|[A-Za-z]{2,3}_[A-Za-z]{2,3}(\.UTF-8)?)$`)
	kernelPackageRE  = regexp.MustCompile(`^linux($|-lts($|-headers$)|-zen($|-headers$)|-hardened($|-headers$)|-headers$)`)
	flatpakIDPartRE  = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

type Config struct {
	SchemaVersion int             `toml:"schema_version"`
	Reference     ReferenceConfig `toml:"reference"`
	Packages      PackageConfig   `toml:"packages"`
	Services      ServiceConfig   `toml:"services"`
	Mirrors       MirrorConfig    `toml:"mirrors"`
	Pacman        PacmanConfig    `toml:"pacman"`
	Flatpak       FlatpakConfig   `toml:"flatpak"`
	Greeter       GreeterConfig   `toml:"greeter"`
	Mouse         MouseConfig     `toml:"mouse"`
	Desktop       ComponentConfig `toml:"desktop"`
	Audio         ComponentConfig `toml:"audio"`
	Kernel        ComponentConfig `toml:"kernel"`
	Fonts         PackageSection  `toml:"fonts"`
	Locale        LocaleConfig    `toml:"locale"`
	Python        PythonConfig    `toml:"python"`
	Node          NodeConfig      `toml:"node"`
	Rust          RustConfig      `toml:"rust"`
	Golang        PackageSection  `toml:"golang"`
}

type ReferenceConfig struct {
	// PackageFiles are TOML inventory files resolved relative to zacman.toml.
	PackageFiles []string `toml:"package_files"`
}

type Inventory struct {
	Repo, Foreign                                                                   []string
	Flatpak, FlatpakUser                                                            []string
	GreeterPkgs                                                                     []string
	GreeterSystem, GreeterTheme, GreeterThemeSource, GreeterThemePackage            string
	MousePkgs                                                                       []string
	MouseTheme, MouseThemeSource, MouseThemePackage                                 string
	MouseSize                                                                       int
	DesktopPkgs, DesktopConfig                                                      []string
	AudioPkgs, AudioConfig                                                          []string
	KernelPkgs, KernelConfig                                                        []string
	FontPkgs, FontConfig                                                            []string
	SystemLanguage, KeyboardLayout, KeyboardVariant, KeyboardModel, KeyboardOptions string
	PythonUV, PythonPip3, NodeNPM, RustCargo, Golang                                []string
	PythonUVConfig, PythonPip3Config, NodeNPMConfig, RustCargoConfig, GolangConfig  []string
}

type PackageConfig struct {
	Repo          []string             `toml:"repo"`
	IgnoreUpdates []string             `toml:"ignore_updates"`
	Foreign       ForeignPackageConfig `toml:"foreign"`
	AUR           AURConfig            `toml:"aur"`
}

type ForeignPackageConfig struct {
	// Items records installed packages not found in configured sync databases.
	// They are inventory-only: Zacman does not install/remove them as repo packages.
	Items []string `toml:"items"`
}

type MirrorConfig struct {
	Servers          []string `toml:"servers"`
	BlackarchServers []string `toml:"blackarch_servers"`
}

type PacmanConfig struct {
	ILoveCandy bool `toml:"ilove_candy"`
}

type PackageSection struct {
	Pkgs   []string `toml:"pkgs"`
	Config []string `toml:"config"`
}

type ComponentConfig = PackageSection

type FlatpakConfig struct {
	Pkgs     []string `toml:"pkgs"`
	UserPkgs []string `toml:"user_pkgs"`
	Remote   string   `toml:"remote"`
	Scope    string   `toml:"scope"`
}

type GreeterConfig struct {
	Pkgs         []string `toml:"pkgs"`
	System       string   `toml:"system"`
	Theme        string   `toml:"theme"`
	ThemeSource  string   `toml:"theme_source"`
	ThemePackage string   `toml:"theme_package"`
}

// MouseConfig manages cursor packages and the desktop cursor theme. Theme names
// are preferred; absolute paths are accepted only under standard icon-theme roots.
// Size 0 means preserve the currently configured cursor size.
type MouseConfig struct {
	Pkgs         []string `toml:"pkgs"`
	Theme        string   `toml:"theme"`
	ThemeSource  string   `toml:"theme_source"`
	ThemePackage string   `toml:"theme_package"`
	Size         int      `toml:"size"`
}

type LocaleConfig struct {
	SystemLanguage  string `toml:"system_language"`
	KeyboardLayout  string `toml:"keyboard_layout"`
	KeyboardVariant string `toml:"keyboard_variant"`
	KeyboardModel   string `toml:"keyboard_model"`
	KeyboardOptions string `toml:"keyboard_options"`
}

type PythonConfig struct {
	UV   PackageSection `toml:"uv"`
	Pip3 PackageSection `toml:"pip3"`
}

type NodeConfig struct {
	NPM PackageSection `toml:"npm"`
}

type RustConfig struct {
	Cargo PackageSection `toml:"cargo"`
}

type AURConfig struct {
	Enabled bool     `toml:"enabled"`
	Items   []string `toml:"items"`
}

type ServiceConfig struct {
	Enable  []string `toml:"enable"`
	Disable []string `toml:"disable"`
}

func Default() Config {
	return Config{
		SchemaVersion: SchemaVersion,
		Packages: PackageConfig{
			Repo:          []string{},
			IgnoreUpdates: []string{},
			Foreign:       ForeignPackageConfig{Items: []string{}},
			AUR:           AURConfig{Enabled: false, Items: []string{}},
		},
		Services: ServiceConfig{Enable: []string{}, Disable: []string{}},
		Mirrors:  MirrorConfig{Servers: []string{}, BlackarchServers: []string{}},
		Pacman:   PacmanConfig{ILoveCandy: true},
		Flatpak:  FlatpakConfig{Pkgs: []string{}, UserPkgs: []string{}, Remote: "flathub", Scope: "system"},
		Greeter:  GreeterConfig{Pkgs: []string{}, ThemeSource: ""},
		Mouse:    MouseConfig{Pkgs: []string{}},
		Desktop:  ComponentConfig{Pkgs: []string{}, Config: []string{}},
		Audio:    ComponentConfig{Pkgs: []string{}, Config: []string{}},
		Kernel:   ComponentConfig{Pkgs: []string{}, Config: []string{}},
		Fonts:    PackageSection{Pkgs: []string{}, Config: []string{}},
		Locale:   LocaleConfig{},
		Python:   PythonConfig{UV: PackageSection{Pkgs: []string{}, Config: []string{}}, Pip3: PackageSection{Pkgs: []string{}, Config: []string{}}},
		Node:     NodeConfig{NPM: PackageSection{Pkgs: []string{}, Config: []string{}}},
		Rust:     RustConfig{Cargo: PackageSection{Pkgs: []string{}, Config: []string{}}},
		Golang:   PackageSection{Pkgs: []string{}, Config: []string{}},
		// Package inventories can still be referenced explicitly for backward
		// compatibility, but fresh manifests keep everything in zacman.toml.
		Reference: ReferenceConfig{PackageFiles: []string{}},
	}
}

// ResolveSDDMTheme accepts either a theme directory name or an absolute path
// to a directory inside SDDM's standard theme root. SDDM's Current setting
// requires the directory name, so callers should write name to the SDDM config
// and use dir when checking the local theme directory.
func ResolveSDDMTheme(theme string) (name, dir string, err error) {
	const themeRoot = "/usr/share/sddm/themes"
	if theme == "" || strings.TrimSpace(theme) != theme || strings.ContainsAny(theme, "\x00\r\n") {
		return "", "", errors.New("expected a theme directory name or an absolute path inside /usr/share/sddm/themes")
	}
	root := filepath.Clean(themeRoot)
	if filepath.IsAbs(theme) {
		clean := filepath.Clean(theme)
		rel, relErr := filepath.Rel(root, clean)
		if relErr != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			return "", "", fmt.Errorf("absolute theme path must point to a theme directory inside %s", root)
		}
		name = filepath.Base(clean)
		if !themeNameRE.MatchString(name) {
			return "", "", errors.New("theme directory name may contain only letters, digits, dot, underscore, plus, and hyphen")
		}
		return name, clean, nil
	}
	if !themeNameRE.MatchString(theme) {
		return "", "", errors.New("use a theme directory name or an absolute path inside /usr/share/sddm/themes")
	}
	return theme, filepath.Join(root, theme), nil
}

// ResolveCursorTheme accepts a cursor theme directory name or an absolute path
// under /usr/share/icons, /usr/local/share/icons, ~/.local/share/icons, or ~/.icons.
// It returns a preferred lookup directory for a bare name even if not present yet,
// which allows a repo-provided theme to be installed before applying it.
func ResolveCursorTheme(theme string) (name, dir string, err error) {
	if theme == "" || strings.TrimSpace(theme) != theme || strings.ContainsAny(theme, "\x00\r\n") {
		return "", "", errors.New("expected a cursor theme directory name or an absolute path inside an icon-theme directory")
	}
	home := cursorTargetHome()
	roots := []string{"/usr/share/icons", "/usr/local/share/icons"}
	if home != "" {
		roots = append([]string{filepath.Join(home, ".local", "share", "icons"), filepath.Join(home, ".icons")}, roots...)
	}
	if filepath.IsAbs(theme) {
		clean := filepath.Clean(theme)
		if clean != theme {
			return "", "", errors.New("cursor theme path must be canonical and must not contain dot-segments")
		}
		allowed := false
		for _, root := range roots {
			rel, relErr := filepath.Rel(filepath.Clean(root), clean)
			if relErr == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", "", errors.New("absolute cursor theme path must be inside /usr/share/icons, /usr/local/share/icons, ~/.local/share/icons, or ~/.icons")
		}
		name = filepath.Base(clean)
		if !themeNameRE.MatchString(name) {
			return "", "", errors.New("cursor theme directory name may contain only letters, digits, dot, underscore, plus, and hyphen")
		}
		return name, clean, nil
	}
	if strings.ContainsAny(theme, "/\\") || !themeNameRE.MatchString(theme) {
		return "", "", errors.New("use a cursor theme directory name or an absolute path inside a supported icon-theme directory")
	}
	for _, root := range roots {
		candidate := filepath.Join(root, theme)
		if info, statErr := os.Lstat(candidate); statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return theme, candidate, nil
		}
	}
	return theme, filepath.Join("/usr/share/icons", theme), nil
}

func cursorTargetHome() string {
	if sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER")); sudoUser != "" && sudoUser != "root" {
		if account, err := osuser.Lookup(sudoUser); err == nil {
			return account.HomeDir
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}

func Decode(data []byte) (Config, error) {
	cfg, err := parseTOML(data)
	if err != nil {
		return Config{}, fmt.Errorf("parse TOML: %w", err)
	}
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// LoadBase reads only zacman.toml. It is intended for operations that modify the
// primary manifest without copying referenced inventory entries into it.
func LoadBase(path string) (Config, error) {
	data, err := ReadRaw(path)
	if err != nil {
		return Config{}, err
	}
	path = normalizeConfigPath(path)
	cfg, err := Decode(data)
	if err != nil {
		return Config{}, fmt.Errorf("config %q: %w", path, err)
	}
	return cfg, nil
}

// Load reads the main manifest and expands every referenced package inventory.
// The returned Config is the effective desired state; use LoadBase before writing
// zacman.toml so imported packages remain in their separate files.
func Load(path string) (Config, error) {
	cfg, err := LoadBase(path)
	if err != nil {
		return Config{}, err
	}
	path = normalizeConfigPath(path)
	mainName := filepath.Base(path)
	repo := make(map[string]struct{}, len(cfg.Packages.Repo))
	foreign := make(map[string]struct{}, len(cfg.Packages.Foreign.Items))
	for _, pkg := range cfg.Packages.Repo {
		repo[pkg] = struct{}{}
	}
	for _, pkg := range cfg.Packages.Foreign.Items {
		foreign[pkg] = struct{}{}
	}
	for _, name := range cfg.Reference.PackageFiles {
		if name == mainName {
			return Config{}, fmt.Errorf("reference.package_files: %q refers to the main configuration itself", name)
		}
		inventoryPath := filepath.Join(filepath.Dir(path), name)
		inventory, err := ReadInventory(inventoryPath)
		if err != nil {
			return Config{}, fmt.Errorf("reference.package_files %q: %w", name, err)
		}
		for _, pkg := range inventory.Repo {
			repo[pkg] = struct{}{}
		}
		for _, pkg := range inventory.Foreign {
			foreign[pkg] = struct{}{}
		}
		cfg.Flatpak.Pkgs = append(cfg.Flatpak.Pkgs, inventory.Flatpak...)
		cfg.Flatpak.UserPkgs = append(cfg.Flatpak.UserPkgs, inventory.FlatpakUser...)
		cfg.Greeter.Pkgs = append(cfg.Greeter.Pkgs, inventory.GreeterPkgs...)
		cfg.Mouse.Pkgs = append(cfg.Mouse.Pkgs, inventory.MousePkgs...)
		cfg.Desktop.Pkgs = append(cfg.Desktop.Pkgs, inventory.DesktopPkgs...)
		cfg.Desktop.Config = append(cfg.Desktop.Config, inventory.DesktopConfig...)
		cfg.Audio.Pkgs = append(cfg.Audio.Pkgs, inventory.AudioPkgs...)
		cfg.Audio.Config = append(cfg.Audio.Config, inventory.AudioConfig...)
		cfg.Kernel.Pkgs = append(cfg.Kernel.Pkgs, inventory.KernelPkgs...)
		cfg.Kernel.Config = append(cfg.Kernel.Config, inventory.KernelConfig...)
		cfg.Fonts.Pkgs = append(cfg.Fonts.Pkgs, inventory.FontPkgs...)
		cfg.Fonts.Config = append(cfg.Fonts.Config, inventory.FontConfig...)
		cfg.Python.UV.Pkgs = append(cfg.Python.UV.Pkgs, inventory.PythonUV...)
		cfg.Python.UV.Config = append(cfg.Python.UV.Config, inventory.PythonUVConfig...)
		cfg.Python.Pip3.Pkgs = append(cfg.Python.Pip3.Pkgs, inventory.PythonPip3...)
		cfg.Python.Pip3.Config = append(cfg.Python.Pip3.Config, inventory.PythonPip3Config...)
		cfg.Node.NPM.Pkgs = append(cfg.Node.NPM.Pkgs, inventory.NodeNPM...)
		cfg.Node.NPM.Config = append(cfg.Node.NPM.Config, inventory.NodeNPMConfig...)
		cfg.Rust.Cargo.Pkgs = append(cfg.Rust.Cargo.Pkgs, inventory.RustCargo...)
		cfg.Rust.Cargo.Config = append(cfg.Rust.Cargo.Config, inventory.RustCargoConfig...)
		cfg.Golang.Pkgs = append(cfg.Golang.Pkgs, inventory.Golang...)
		cfg.Golang.Config = append(cfg.Golang.Config, inventory.GolangConfig...)
		if cfg.Greeter.System == "" {
			cfg.Greeter.System = inventory.GreeterSystem
		}
		// SDDM theme metadata is only meaningful when the effective greeter is
		// SDDM. Do not resurrect a stale SDDM theme after the user switches to
		// GDM, LightDM, greetd, ly, or emptty.
		if cfg.Greeter.System == "sddm" {
			if cfg.Greeter.Theme == "" {
				cfg.Greeter.Theme = inventory.GreeterTheme
			}
			if cfg.Greeter.ThemeSource == "" {
				cfg.Greeter.ThemeSource = inventory.GreeterThemeSource
			}
			if cfg.Greeter.ThemePackage == "" {
				cfg.Greeter.ThemePackage = inventory.GreeterThemePackage
			}
		}
		if cfg.Mouse.Theme == "" {
			cfg.Mouse.Theme = inventory.MouseTheme
		}
		if cfg.Mouse.ThemeSource == "" {
			cfg.Mouse.ThemeSource = inventory.MouseThemeSource
		}
		if cfg.Mouse.ThemePackage == "" {
			cfg.Mouse.ThemePackage = inventory.MouseThemePackage
		}
		if cfg.Mouse.Size == 0 {
			cfg.Mouse.Size = inventory.MouseSize
		}
		if cfg.Locale.SystemLanguage == "" {
			cfg.Locale.SystemLanguage = inventory.SystemLanguage
		}
		if cfg.Locale.KeyboardLayout == "" {
			cfg.Locale.KeyboardLayout = inventory.KeyboardLayout
		}
		if cfg.Locale.KeyboardVariant == "" {
			cfg.Locale.KeyboardVariant = inventory.KeyboardVariant
		}
		if cfg.Locale.KeyboardModel == "" {
			cfg.Locale.KeyboardModel = inventory.KeyboardModel
		}
		if cfg.Locale.KeyboardOptions == "" {
			cfg.Locale.KeyboardOptions = inventory.KeyboardOptions
		}
	}
	// A package present in any repo list takes precedence over an old foreign
	// inventory record, e.g. when a package later enters an official repository.
	for pkg := range repo {
		delete(foreign, pkg)
	}
	cfg.Packages.Repo = sortedMapKeys(repo)
	cfg.Packages.Foreign.Items = sortedMapKeys(foreign)
	cfg.Flatpak.Pkgs = SortedUnique(cfg.Flatpak.Pkgs)
	cfg.Flatpak.UserPkgs = SortedUnique(cfg.Flatpak.UserPkgs)
	cfg.Greeter.Pkgs = SortedUnique(cfg.Greeter.Pkgs)
	cfg.Mouse.Pkgs = SortedUnique(cfg.Mouse.Pkgs)
	cfg.Desktop.Pkgs = SortedUnique(cfg.Desktop.Pkgs)
	cfg.Desktop.Config = SortedUnique(cfg.Desktop.Config)
	cfg.Audio.Pkgs = SortedUnique(cfg.Audio.Pkgs)
	cfg.Audio.Config = SortedUnique(cfg.Audio.Config)
	cfg.Kernel.Pkgs = SortedUnique(cfg.Kernel.Pkgs)
	cfg.Kernel.Config = SortedUnique(cfg.Kernel.Config)
	cfg.Fonts.Pkgs = SortedUnique(cfg.Fonts.Pkgs)
	cfg.Fonts.Config = SortedUnique(cfg.Fonts.Config)
	cfg.Python.UV.Pkgs = SortedUnique(cfg.Python.UV.Pkgs)
	cfg.Python.UV.Config = SortedUnique(cfg.Python.UV.Config)
	cfg.Python.Pip3.Pkgs = SortedUnique(cfg.Python.Pip3.Pkgs)
	cfg.Python.Pip3.Config = SortedUnique(cfg.Python.Pip3.Config)
	cfg.Node.NPM.Pkgs = SortedUnique(cfg.Node.NPM.Pkgs)
	cfg.Node.NPM.Config = SortedUnique(cfg.Node.NPM.Config)
	cfg.Rust.Cargo.Pkgs = SortedUnique(cfg.Rust.Cargo.Pkgs)
	cfg.Rust.Cargo.Config = SortedUnique(cfg.Rust.Cargo.Config)
	cfg.Golang.Pkgs = SortedUnique(cfg.Golang.Pkgs)
	cfg.Golang.Config = SortedUnique(cfg.Golang.Config)
	if err := Validate(cfg); err != nil {
		return Config{}, fmt.Errorf("effective configuration: %w", err)
	}
	return cfg, nil
}

func sortedMapKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	return SortedUnique(out)
}

func normalizeConfigPath(path string) string {
	if path == "" {
		return DefaultPath
	}
	return path
}

// ConfigDirectory returns the directory that owns the main manifest and its
// referenced inventory files.
func ConfigDirectory(path string) string {
	return filepath.Dir(normalizeConfigPath(path))
}

// ReferenceFilePath returns an already-validated reference path inside the
// manifest directory. Callers should obtain the filename from ReferenceConfig.
func ReferenceFilePath(path, name string) string {
	return filepath.Join(ConfigDirectory(path), name)
}

// ReadRaw safely reads the active manifest without parsing it, so users can
// inspect and repair a malformed config with `zacman config show`.
func ReadRaw(path string) ([]byte, error) {
	if path == "" {
		path = DefaultPath
	}
	parent := filepath.Dir(path)
	parentInfo, parentErr := os.Lstat(parent)
	if parentErr == nil {
		if err := validateParentDirectory(parent, parentInfo); err != nil {
			return nil, err
		}
	}
	if parentErr != nil && !errors.Is(parentErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect config directory: %w", parentErr)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing to read config through symlink: %s", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("config is not a regular file: %s", path)
	}
	if err := validateConfigFile(path, info); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	return data, nil
}

func Validate(cfg Config) error {
	if cfg.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version: unsupported version %d (supported: %d)", cfg.SchemaVersion, SchemaVersion)
	}
	if err := validateReferenceFiles(cfg.Reference.PackageFiles); err != nil {
		return err
	}
	if err := uniqueAndValid("packages.repo", cfg.Packages.Repo, packageNameRE); err != nil {
		return err
	}
	if err := uniqueAndValid("packages.ignore_updates", cfg.Packages.IgnoreUpdates, packageNameRE); err != nil {
		return err
	}
	if err := uniqueAndValid("packages.aur.items", cfg.Packages.AUR.Items, packageNameRE); err != nil {
		return err
	}
	if err := uniqueAndValid("packages.foreign.items", cfg.Packages.Foreign.Items, packageNameRE); err != nil {
		return err
	}
	// Component lists that are fed to Pacman must use ordinary Arch package
	// names; only ecosystem-manager lists accept module paths and version specs.
	for _, item := range []struct {
		name   string
		values []string
	}{
		{"greeter.pkgs", cfg.Greeter.Pkgs}, {"mouse.pkgs", cfg.Mouse.Pkgs}, {"desktop.pkgs", cfg.Desktop.Pkgs},
		{"audio.pkgs", cfg.Audio.Pkgs}, {"kernel.pkgs", cfg.Kernel.Pkgs}, {"fonts.pkgs", cfg.Fonts.Pkgs},
	} {
		if err := uniqueAndValid(item.name, item.values, packageNameRE); err != nil {
			return err
		}
	}
	for _, item := range []struct {
		name   string
		values []string
	}{
		{"flatpak.pkgs", cfg.Flatpak.Pkgs}, {"flatpak.user_pkgs", cfg.Flatpak.UserPkgs},
		{"python.uv.pkgs", cfg.Python.UV.Pkgs}, {"python.pip3.pkgs", cfg.Python.Pip3.Pkgs},
		{"node.npm.pkgs", cfg.Node.NPM.Pkgs}, {"rust.cargo.pkgs", cfg.Rust.Cargo.Pkgs}, {"golang.pkgs", cfg.Golang.Pkgs},
	} {
		if err := uniqueAndValid(item.name, item.values, managerPackageRE); err != nil {
			return err
		}
	}
	for _, item := range []struct {
		name   string
		values []string
	}{
		{"desktop.config", cfg.Desktop.Config}, {"audio.config", cfg.Audio.Config}, {"kernel.config", cfg.Kernel.Config}, {"fonts.config", cfg.Fonts.Config},
		{"python.uv.config", cfg.Python.UV.Config}, {"python.pip3.config", cfg.Python.Pip3.Config}, {"node.npm.config", cfg.Node.NPM.Config}, {"rust.cargo.config", cfg.Rust.Cargo.Config}, {"golang.config", cfg.Golang.Config},
	} {
		if err := validateConfigMappings(item.name, item.values); err != nil {
			return err
		}
	}
	if cfg.Flatpak.Remote == "" || strings.TrimSpace(cfg.Flatpak.Remote) != cfg.Flatpak.Remote || strings.ContainsAny(cfg.Flatpak.Remote, " /\\	\r\n") || strings.HasPrefix(cfg.Flatpak.Remote, "-") {
		return errors.New("flatpak.remote: expected a single remote name (default: flathub)")
	}
	if cfg.Flatpak.Scope != "system" && cfg.Flatpak.Scope != "user" {
		return errors.New("flatpak.scope: must be either system or user")
	}
	if cfg.Greeter.System != "" && !containsString([]string{"sddm", "gdm", "lightdm", "greetd", "ly", "emptty"}, cfg.Greeter.System) {
		return fmt.Errorf("greeter.system: unsupported greeter %q (supported: sddm, gdm, lightdm, greetd, ly, emptty)", cfg.Greeter.System)
	}
	if cfg.Greeter.ThemeSource != "" && cfg.Greeter.ThemeSource != "local" && cfg.Greeter.ThemeSource != "repo" {
		return errors.New("greeter.theme_source: must be local or repo")
	}
	if cfg.Greeter.Theme != "" && cfg.Greeter.System != "sddm" {
		return errors.New("greeter.theme: automated theme selection currently requires greeter.system = \"sddm\"")
	}
	if cfg.Greeter.Theme != "" {
		if _, _, err := ResolveSDDMTheme(cfg.Greeter.Theme); err != nil {
			return fmt.Errorf("greeter.theme: %w", err)
		}
		if cfg.Greeter.ThemeSource == "repo" && filepath.IsAbs(cfg.Greeter.Theme) {
			return errors.New("greeter.theme: absolute paths are only supported when theme_source = \"local\"")
		}
	}
	if cfg.Greeter.ThemePackage != "" && !packageNameRE.MatchString(cfg.Greeter.ThemePackage) {
		return fmt.Errorf("greeter.theme_package: invalid Pacman package name %q", cfg.Greeter.ThemePackage)
	}
	if cfg.Greeter.ThemeSource == "repo" && cfg.Greeter.ThemePackage == "" && cfg.Greeter.Theme != "" {
		return errors.New("greeter.theme_package: set the Pacman package that provides the theme when theme_source = \"repo\"")
	}
	if cfg.Mouse.ThemeSource != "" && cfg.Mouse.ThemeSource != "local" && cfg.Mouse.ThemeSource != "repo" {
		return errors.New("mouse.theme_source: must be local or repo")
	}
	if cfg.Mouse.Theme != "" {
		if cfg.Mouse.ThemeSource == "" {
			return errors.New("mouse.theme_source: set to local or repo when mouse.theme is configured")
		}
		if _, _, err := ResolveCursorTheme(cfg.Mouse.Theme); err != nil {
			return fmt.Errorf("mouse.theme: %w", err)
		}
		if cfg.Mouse.ThemeSource == "repo" && filepath.IsAbs(cfg.Mouse.Theme) {
			return errors.New("mouse.theme: absolute paths are only supported when theme_source = \"local\"")
		}
	}
	if cfg.Mouse.ThemePackage != "" && !packageNameRE.MatchString(cfg.Mouse.ThemePackage) {
		return fmt.Errorf("mouse.theme_package: invalid Pacman package name %q", cfg.Mouse.ThemePackage)
	}
	if cfg.Mouse.ThemeSource == "repo" && cfg.Mouse.Theme != "" && cfg.Mouse.ThemePackage == "" {
		return errors.New("mouse.theme_package: set the Pacman package that provides the cursor theme when theme_source = \"repo\"")
	}
	if cfg.Mouse.Size != 0 && (cfg.Mouse.Size < 8 || cfg.Mouse.Size > 128) {
		return errors.New("mouse.size: must be 0 (preserve current size) or between 8 and 128 pixels")
	}
	if cfg.Locale.SystemLanguage != "" && !localeNameRE.MatchString(cfg.Locale.SystemLanguage) {
		return fmt.Errorf("locale.system_language: invalid locale %q (expected C, POSIX, or a UTF-8 locale such as en_US.UTF-8)", cfg.Locale.SystemLanguage)
	}
	if cfg.Locale.SystemLanguage != "" && cfg.Locale.SystemLanguage != "C" && cfg.Locale.SystemLanguage != "POSIX" && !strings.HasSuffix(cfg.Locale.SystemLanguage, ".UTF-8") {
		return fmt.Errorf("locale.system_language: non-C locales must use UTF-8 encoding, e.g. en_US.UTF-8")
	}
	if (cfg.Locale.KeyboardVariant != "" || cfg.Locale.KeyboardModel != "" || cfg.Locale.KeyboardOptions != "") && cfg.Locale.KeyboardLayout == "" {
		return errors.New("locale.keyboard_layout: required when keyboard_variant, keyboard_model, or keyboard_options is set")
	}
	for name, value := range map[string]string{"locale.keyboard_layout": cfg.Locale.KeyboardLayout, "locale.keyboard_variant": cfg.Locale.KeyboardVariant, "locale.keyboard_model": cfg.Locale.KeyboardModel, "locale.keyboard_options": cfg.Locale.KeyboardOptions} {
		if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n\x00 ;") {
			return fmt.Errorf("%s: invalid value %q", name, value)
		}
	}
	repoSet := make(map[string]struct{}, len(cfg.Packages.Repo))
	for _, pkg := range cfg.Packages.Repo {
		repoSet[pkg] = struct{}{}
	}
	for _, pkg := range cfg.Packages.Foreign.Items {
		if _, exists := repoSet[pkg]; exists {
			return fmt.Errorf("packages.foreign.items: %q also appears in packages.repo", pkg)
		}
	}
	if err := validateMirrorServers("mirrors.servers", cfg.Mirrors.Servers); err != nil {
		return err
	}
	if err := validateMirrorServers("mirrors.blackarch_servers", cfg.Mirrors.BlackarchServers); err != nil {
		return err
	}
	if err := uniqueAndValid("services.enable", cfg.Services.Enable, unitNameRE); err != nil {
		return err
	}
	if err := uniqueAndValid("services.disable", cfg.Services.Disable, unitNameRE); err != nil {
		return err
	}
	enabled := make(map[string]struct{}, len(cfg.Services.Enable))
	for _, unit := range cfg.Services.Enable {
		enabled[unit] = struct{}{}
	}
	for _, unit := range cfg.Services.Disable {
		if _, ok := enabled[unit]; ok {
			return fmt.Errorf("services: %q appears in both enable and disable", unit)
		}
	}
	if cfg.Greeter.System != "" {
		greeterUnit := cfg.Greeter.System + ".service"
		if containsString(cfg.Services.Disable, greeterUnit) {
			return fmt.Errorf("services.disable: %s conflicts with greeter.system = %q, which enables the selected greeter", greeterUnit, cfg.Greeter.System)
		}
	}
	return nil
}

func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// validateConfigMappings accepts entries in the form source=>destination. Sources
// are relative paths beneath the Zacman config directory; destinations are /etc
// paths or ~/.config paths. They are copied only during an explicitly confirmed init.
func validateConfigMappings(field string, values []string) error {
	seen := map[string]bool{}
	for i, item := range values {
		parts := strings.Split(item, "=>")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != parts[0] || strings.TrimSpace(parts[1]) != parts[1] || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("%s[%d]: expected source=>destination, e.g. profiles/niri/config.kdl=>~/.config/niri/config.kdl", field, i)
		}
		src := filepath.Clean(parts[0])
		if filepath.IsAbs(src) || src == "." || src == ".." || strings.HasPrefix(src, ".."+string(filepath.Separator)) || strings.ContainsAny(item, "\r\n\x00") {
			return fmt.Errorf("%s[%d]: source must be a relative path inside the Zacman config directory", field, i)
		}
		dst := parts[1]
		if !(strings.HasPrefix(dst, "/etc/") || strings.HasPrefix(dst, "~/.config/")) || strings.Contains(dst, "..") || strings.ContainsAny(dst, "\r\n\x00") {
			return fmt.Errorf("%s[%d]: destination must be under /etc/ or ~/.config/", field, i)
		}
		if seen[dst] {
			return fmt.Errorf("%s: multiple entries target %q", field, dst)
		}
		seen[dst] = true
	}
	return nil
}

// AllPacmanPackages returns the unique package set managed by Pacman across
// ordinary and specialized system sections. Package managers remain separate.
func AllPacmanPackages(cfg Config) []string {
	pkgs := append([]string{}, cfg.Packages.Repo...)
	pkgs = append(pkgs, cfg.Greeter.Pkgs...)
	pkgs = append(pkgs, cfg.Mouse.Pkgs...)
	pkgs = append(pkgs, cfg.Desktop.Pkgs...)
	pkgs = append(pkgs, cfg.Audio.Pkgs...)
	pkgs = append(pkgs, cfg.Kernel.Pkgs...)
	pkgs = append(pkgs, cfg.Fonts.Pkgs...)
	if cfg.Greeter.System != "" {
		greeterPackages := map[string]string{"sddm": "sddm", "gdm": "gdm", "lightdm": "lightdm", "greetd": "greetd", "ly": "ly", "emptty": "emptty"}
		if pkg := greeterPackages[cfg.Greeter.System]; pkg != "" {
			pkgs = append(pkgs, pkg)
		}
	}
	if cfg.Greeter.ThemeSource == "repo" && cfg.Greeter.ThemePackage != "" {
		pkgs = append(pkgs, cfg.Greeter.ThemePackage)
	}
	if cfg.Mouse.ThemeSource == "repo" && cfg.Mouse.ThemePackage != "" {
		pkgs = append(pkgs, cfg.Mouse.ThemePackage)
	}
	return SortedUnique(pkgs)
}

// CategoryWarnings recommends keeping specialist stacks in their dedicated
// TOML groups. It does not reject a package list because Arch package names can
// be ambiguous and the warning is advisory only.
// OrganizePackages moves recognizable specialized packages out of packages.repo
// into their dedicated sections. It changes configuration only; it never installs
// or removes anything. The movement map is suitable for concise doctor output.
func OrganizePackages(cfg Config) (Config, map[string][]string) {
	moved := map[string][]string{}
	kept := make([]string, 0, len(cfg.Packages.Repo))
	known := map[string]string{}
	for _, section := range []struct {
		name string
		pkgs []string
	}{
		{"greeter", cfg.Greeter.Pkgs}, {"mouse", cfg.Mouse.Pkgs}, {"desktop", cfg.Desktop.Pkgs}, {"audio", cfg.Audio.Pkgs},
		{"kernel", cfg.Kernel.Pkgs}, {"fonts", cfg.Fonts.Pkgs}, {"flatpak", cfg.Flatpak.Pkgs},
	} {
		for _, pkg := range section.pkgs {
			if _, exists := known[pkg]; !exists {
				known[pkg] = section.name
			}
		}
	}
	for _, pkg := range cfg.Packages.Repo {
		category := RecommendedPackageCategory(pkg)
		if category == "" && looksLikeFlatpakID(pkg) {
			category = "flatpak"
		}
		if category == "" {
			if existing, ok := known[pkg]; ok {
				moved[existing] = append(moved[existing], pkg+" (removed duplicate from packages.repo)")
				continue
			}
			kept = append(kept, pkg)
			continue
		}
		switch category {
		case "greeter":
			cfg.Greeter.Pkgs = append(cfg.Greeter.Pkgs, pkg)
		case "mouse":
			cfg.Mouse.Pkgs = append(cfg.Mouse.Pkgs, pkg)
		case "desktop":
			cfg.Desktop.Pkgs = append(cfg.Desktop.Pkgs, pkg)
		case "audio":
			cfg.Audio.Pkgs = append(cfg.Audio.Pkgs, pkg)
		case "kernel":
			cfg.Kernel.Pkgs = append(cfg.Kernel.Pkgs, pkg)
		case "fonts":
			cfg.Fonts.Pkgs = append(cfg.Fonts.Pkgs, pkg)
		case "flatpak":
			cfg.Flatpak.Pkgs = append(cfg.Flatpak.Pkgs, pkg)
		default:
			kept = append(kept, pkg)
			continue
		}
		if known[pkg] == "" || known[pkg] == category {
			moved[category] = append(moved[category], pkg)
		} else {
			moved[category] = append(moved[category], pkg+" (moved from packages.repo; was also in ["+known[pkg]+"].pkgs)")
		}
	}
	cfg.Packages.Repo = SortedUnique(kept)
	cfg.Greeter.Pkgs = SortedUnique(cfg.Greeter.Pkgs)
	cfg.Mouse.Pkgs = SortedUnique(cfg.Mouse.Pkgs)
	cfg.Desktop.Pkgs = SortedUnique(cfg.Desktop.Pkgs)
	cfg.Audio.Pkgs = SortedUnique(cfg.Audio.Pkgs)
	cfg.Kernel.Pkgs = SortedUnique(cfg.Kernel.Pkgs)
	cfg.Fonts.Pkgs = SortedUnique(cfg.Fonts.Pkgs)
	cfg.Flatpak.Pkgs = SortedUnique(cfg.Flatpak.Pkgs)
	for key := range moved {
		moved[key] = SortedUnique(moved[key])
	}
	return cfg, moved
}

func CategoryWarnings(cfg Config) []string {
	warnings := []string{}
	seen := map[string]bool{}
	managerOwners := map[string][]string{}
	for _, section := range []struct {
		name string
		pkgs []string
	}{
		{"flatpak.pkgs", cfg.Flatpak.Pkgs}, {"flatpak.user_pkgs", cfg.Flatpak.UserPkgs},
		{"greeter.pkgs", cfg.Greeter.Pkgs}, {"mouse.pkgs", cfg.Mouse.Pkgs}, {"desktop.pkgs", cfg.Desktop.Pkgs},
		{"audio.pkgs", cfg.Audio.Pkgs}, {"kernel.pkgs", cfg.Kernel.Pkgs}, {"fonts.pkgs", cfg.Fonts.Pkgs},
		{"python.uv.pkgs", cfg.Python.UV.Pkgs}, {"python.pip3.pkgs", cfg.Python.Pip3.Pkgs},
		{"node.npm.pkgs", cfg.Node.NPM.Pkgs}, {"rust.cargo.pkgs", cfg.Rust.Cargo.Pkgs}, {"golang.pkgs", cfg.Golang.Pkgs},
	} {
		for _, pkg := range section.pkgs {
			managerOwners[pkg] = append(managerOwners[pkg], section.name)
		}
	}
	for _, pkg := range cfg.Packages.Repo {
		category := RecommendedPackageCategory(pkg)
		if category != "" && !seen[pkg] {
			warnings = append(warnings, fmt.Sprintf("%s looks like a %s package; consider moving it from packages.repo to [%s].pkgs (run 'zacman doctor' to organize packages automatically)", pkg, category, category))
			seen[pkg] = true
		}
		if looksLikeFlatpakID(pkg) && !seen[pkg] {
			warnings = append(warnings, fmt.Sprintf("%s looks like a Flatpak app ID; use [flatpak].pkgs instead of packages.repo (run 'zacman doctor' to organize packages automatically when it can identify them)", pkg))
			seen[pkg] = true
		}
		if owners := managerOwners[pkg]; len(owners) > 0 && !seen[pkg] {
			sort.Strings(owners)
			warnings = append(warnings, fmt.Sprintf("%s is also declared in %s; keep it in its dedicated section instead of packages.repo to avoid duplicate or conflicting installs (run 'zacman doctor' to organize packages automatically)", pkg, strings.Join(owners, ", ")))
			seen[pkg] = true
		}
	}
	sort.Strings(warnings)
	return warnings
}

func looksLikeFlatpakID(pkg string) bool {
	parts := strings.Split(pkg, ".")
	if len(parts) < 3 {
		return false
	}
	switch strings.ToLower(parts[0]) {
	case "org", "com", "io", "net", "app", "dev", "tv", "me", "de", "fr", "uk":
		for _, part := range parts {
			if part == "" || !flatpakIDPartRE.MatchString(part) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func RecommendedPackageCategory(pkg string) string {
	p := strings.ToLower(pkg)
	switch {
	case containsString([]string{"sddm", "gdm", "lightdm", "greetd", "ly", "emptty", "tuigreet"}, p) || strings.HasPrefix(p, "sddm-theme-") || strings.HasPrefix(p, "greetd-"):
		return "greeter"
	case kernelPackageRE.MatchString(p) || p == "linux-firmware" || strings.HasPrefix(p, "linux-firmware-") || p == "amd-ucode" || p == "intel-ucode" || p == "dkms":
		return "kernel"
	case strings.HasPrefix(p, "xcursor-") || strings.HasSuffix(p, "-cursors") || strings.Contains(p, "cursor-theme"):
		return "mouse"
	case strings.HasPrefix(p, "ttf-") || strings.HasPrefix(p, "otf-") || strings.HasPrefix(p, "noto-fonts") || strings.HasSuffix(p, "-fonts") || p == "fontconfig" || p == "freetype2":
		return "fonts"
	case strings.HasPrefix(p, "pipewire") || strings.HasPrefix(p, "pulseaudio") || strings.HasPrefix(p, "alsa-") || strings.HasPrefix(p, "libpipewire") || strings.HasPrefix(p, "libpulse") || containsString([]string{"alsa-utils", "alsa-lib", "wireplumber", "jack2", "qjackctl", "pavucontrol", "easyeffects", "sof-firmware", "mpd", "mpc", "playerctl"}, p):
		return "audio"
	case strings.HasPrefix(p, "plasma-") || strings.HasPrefix(p, "gnome-") || strings.HasPrefix(p, "xfce4-") || strings.HasPrefix(p, "cinnamon") || strings.HasPrefix(p, "mate-") || containsString([]string{"plasma-meta", "kde-applications-meta", "gnome", "xfce4", "mate", "lxqt", "lxqt-session", "budgie", "budgie-desktop", "sway", "niri", "hyprland", "wayfire", "river", "labwc", "i3-wm", "bspwm", "awesome", "openbox", "xorg-server", "xorg-xwayland"}, p):
		return "desktop"
	default:
		return ""
	}
}

func validateMirrorServers(field string, servers []string) error {
	seen := map[string]struct{}{}
	for i, raw := range servers {
		if raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\r\n\t ") {
			return fmt.Errorf("%s[%d]: invalid mirror URL %q", field, i, raw)
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "ftp") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("%s[%d]: expected an http, https, or ftp mirror URL without credentials, query, or fragment", field, i)
		}
		if _, ok := seen[raw]; ok {
			return fmt.Errorf("%s: duplicate mirror URL %q", field, raw)
		}
		seen[raw] = struct{}{}
	}
	return nil
}

// DuplicatePackageEntries reports repeated package names within each package
// list in the primary Zacman manifest. The first occurrence is retained.
// It deliberately parses without full validation so init can repair an invalid
// duplicate list before the regular strict validation path runs.
func DuplicatePackageEntries(path string) ([]string, error) {
	data, err := ReadRaw(path)
	if err != nil {
		return nil, err
	}
	cfg, err := parseTOMLMode(data, true, false)
	if err != nil {
		return nil, fmt.Errorf("config %q: %w", normalizeConfigPath(path), err)
	}
	return collectDuplicatePackageEntries(&cfg), nil
}

// RepairDuplicatePackageEntries removes repeated names from package arrays in
// the primary manifest and preserves all unrelated user formatting/comments.
// The caller owns the confirmation prompt; this method only commits a repair.
func RepairDuplicatePackageEntries(path string) ([]string, error) {
	data, err := ReadRaw(path)
	if err != nil {
		return nil, err
	}
	cfg, err := parseTOMLMode(data, true, false)
	if err != nil {
		return nil, fmt.Errorf("config %q: %w", normalizeConfigPath(path), err)
	}
	duplicates := collectDuplicatePackageEntries(&cfg)
	if len(duplicates) == 0 {
		return nil, nil
	}

	// preserveUserFormatting needs the pre-repair semantic values. Re-parse an
	// encoded copy so its slices cannot alias the config that is about to change.
	oldEncoded, err := encodeTOML(cfg)
	if err != nil {
		return nil, err
	}
	oldCfg, err := parseTOMLMode(oldEncoded, true, false)
	if err != nil {
		return nil, fmt.Errorf("snapshot duplicate config values: %w", err)
	}
	removeDuplicatePackageEntries(&cfg)
	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("config still has validation errors after duplicate cleanup: %w", err)
	}
	encoded, err := encodeTOML(cfg)
	if err != nil {
		return nil, err
	}
	preserved, err := preserveUserFormatting(data, encoded, oldCfg, cfg)
	if err != nil {
		return nil, fmt.Errorf("preserve config formatting while removing duplicates: %w", err)
	}
	if err := writeAtomicBytes(path, preserved); err != nil {
		return nil, fmt.Errorf("write repaired config: %w", err)
	}
	return duplicates, nil
}

type packageListRef struct {
	name   string
	values *[]string
}

func packageLists(cfg *Config) []packageListRef {
	return []packageListRef{
		{"packages.repo", &cfg.Packages.Repo},
		{"packages.ignore_updates", &cfg.Packages.IgnoreUpdates},
		{"packages.foreign.items", &cfg.Packages.Foreign.Items},
		{"packages.aur.items", &cfg.Packages.AUR.Items},
		{"greeter.pkgs", &cfg.Greeter.Pkgs},
		{"mouse.pkgs", &cfg.Mouse.Pkgs},
		{"desktop.pkgs", &cfg.Desktop.Pkgs},
		{"audio.pkgs", &cfg.Audio.Pkgs},
		{"kernel.pkgs", &cfg.Kernel.Pkgs},
		{"fonts.pkgs", &cfg.Fonts.Pkgs},
		{"flatpak.pkgs", &cfg.Flatpak.Pkgs},
		{"flatpak.user_pkgs", &cfg.Flatpak.UserPkgs},
		{"python.uv.pkgs", &cfg.Python.UV.Pkgs},
		{"python.pip3.pkgs", &cfg.Python.Pip3.Pkgs},
		{"node.npm.pkgs", &cfg.Node.NPM.Pkgs},
		{"rust.cargo.pkgs", &cfg.Rust.Cargo.Pkgs},
		{"golang.pkgs", &cfg.Golang.Pkgs},
	}
}

func collectDuplicatePackageEntries(cfg *Config) []string {
	duplicates := []string{}
	for _, list := range packageLists(cfg) {
		seen := map[string]bool{}
		for _, value := range *list.values {
			if seen[value] {
				duplicates = append(duplicates, list.name+": "+value)
			}
			seen[value] = true
		}
	}
	return duplicates
}

func removeDuplicatePackageEntries(cfg *Config) {
	for _, list := range packageLists(cfg) {
		seen := map[string]bool{}
		unique := make([]string, 0, len(*list.values))
		for _, value := range *list.values {
			if seen[value] {
				continue
			}
			seen[value] = true
			unique = append(unique, value)
		}
		*list.values = unique
	}
}

func uniqueAndValid(field string, values []string, re *regexp.Regexp) error {
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		if strings.TrimSpace(value) != value || value == "" || !re.MatchString(value) {
			return fmt.Errorf("%s[%d]: invalid value %q", field, i, value)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s: duplicate value %q", field, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func Marshal(cfg Config) ([]byte, error) {
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	data, err := encodeTOML(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode TOML: %w", err)
	}
	return data, nil
}

// WriteAtomic replaces a TOML file atomically and refuses to replace a symlink.
// It creates the parent directory with restrictive, predictable permissions.
func WriteAtomic(path string, cfg Config) error {
	data, err := Marshal(cfg)
	if err != nil {
		return err
	}
	path = normalizeConfigPath(path)
	// When a privileged command is run through sudo, let the invoking user
	// retain ownership of the default /etc/zacman config tree so subsequent
	// edits and atomic saves work without sudo. Custom --config paths are never
	// silently re-owned.
	if os.Geteuid() == 0 && filepath.Clean(path) == filepath.Clean(DefaultPath) {
		if _, _, ok := sudoTargetIDs(); ok {
			if err := FixOwnership(path); err != nil {
				return fmt.Errorf("make Zacman config editable by the invoking user: %w", err)
			}
		}
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular or symlink config path: %s", path)
		}
		original, readErr := ReadRaw(path)
		if readErr != nil {
			return readErr
		}
		oldCfg, decodeErr := Decode(original)
		if decodeErr != nil {
			return fmt.Errorf("refusing to rewrite invalid config %q: %w", path, decodeErr)
		}
		data, err = preserveUserFormatting(original, data, oldCfg, cfg)
		if err != nil {
			return fmt.Errorf("preserve config formatting: %w", err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect config path: %w", statErr)
	}
	return writeAtomicBytes(path, data)
}

func writeAtomicBytes(path string, data []byte) error {
	path = normalizeConfigPath(path)
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("create config directory %q: %w", parent, err)
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect config directory: %w", err)
	}
	if err := validateParentDirectory(parent, parentInfo); err != nil {
		return err
	}
	var priorInfo os.FileInfo
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to replace config symlink: %s", path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular config path: %s", path)
		}
		if err := validateConfigFile(path, info); err != nil {
			return err
		}
		priorInfo = info
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect config path: %w", err)
	}

	mode := os.FileMode(0644)
	if priorInfo != nil {
		mode = priorInfo.Mode().Perm() & 0644
	}
	tmp, err := os.CreateTemp(parent, ".zacman-config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("set config permissions: %w", err)
	}
	if os.Geteuid() == 0 {
		var uid, gid uint32
		ownerKnown := false
		if priorInfo != nil {
			if stat, ok := priorInfo.Sys().(*syscall.Stat_t); ok {
				uid, gid, ownerKnown = stat.Uid, stat.Gid, true
			}
		} else if stat, ok := parentInfo.Sys().(*syscall.Stat_t); ok {
			uid, gid, ownerKnown = stat.Uid, stat.Gid, true
		}
		if ownerKnown {
			if err := tmp.Chown(int(uid), int(gid)); err != nil {
				tmp.Close()
				return fmt.Errorf("preserve config ownership: %w", err)
			}
		}
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	if dir, err := os.Open(parent); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// DecodeInventory parses the deliberately small format accepted by referenced
// package files. It only permits schema_version, packages.repo, and
// packages.foreign.items so a reference cannot smuggle other settings into the
// primary manifest.
func DecodeInventory(data []byte) (Inventory, error) {
	cfg, err := parseInventoryTOML(data)
	if err != nil {
		return Inventory{}, fmt.Errorf("parse package inventory: %w", err)
	}
	if err := Validate(cfg); err != nil {
		return Inventory{}, err
	}
	return Inventory{
		Repo: cfg.Packages.Repo, Foreign: cfg.Packages.Foreign.Items,
		Flatpak: cfg.Flatpak.Pkgs, FlatpakUser: cfg.Flatpak.UserPkgs,
		GreeterPkgs: cfg.Greeter.Pkgs, GreeterSystem: cfg.Greeter.System, GreeterTheme: cfg.Greeter.Theme,
		GreeterThemeSource: cfg.Greeter.ThemeSource, GreeterThemePackage: cfg.Greeter.ThemePackage,
		MousePkgs: cfg.Mouse.Pkgs, MouseTheme: cfg.Mouse.Theme, MouseThemeSource: cfg.Mouse.ThemeSource, MouseThemePackage: cfg.Mouse.ThemePackage, MouseSize: cfg.Mouse.Size,
		DesktopPkgs: cfg.Desktop.Pkgs, DesktopConfig: cfg.Desktop.Config,
		AudioPkgs: cfg.Audio.Pkgs, AudioConfig: cfg.Audio.Config,
		KernelPkgs: cfg.Kernel.Pkgs, KernelConfig: cfg.Kernel.Config, FontPkgs: cfg.Fonts.Pkgs, FontConfig: cfg.Fonts.Config,
		SystemLanguage: cfg.Locale.SystemLanguage, KeyboardLayout: cfg.Locale.KeyboardLayout,
		KeyboardVariant: cfg.Locale.KeyboardVariant, KeyboardModel: cfg.Locale.KeyboardModel, KeyboardOptions: cfg.Locale.KeyboardOptions,
		PythonUV: cfg.Python.UV.Pkgs, PythonPip3: cfg.Python.Pip3.Pkgs, NodeNPM: cfg.Node.NPM.Pkgs,
		RustCargo: cfg.Rust.Cargo.Pkgs, Golang: cfg.Golang.Pkgs,
		PythonUVConfig: cfg.Python.UV.Config, PythonPip3Config: cfg.Python.Pip3.Config, NodeNPMConfig: cfg.Node.NPM.Config,
		RustCargoConfig: cfg.Rust.Cargo.Config, GolangConfig: cfg.Golang.Config,
	}, nil
}

func ReadInventory(path string) (Inventory, error) {
	data, err := ReadRaw(path)
	if err != nil {
		return Inventory{}, err
	}
	inventory, err := DecodeInventory(data)
	if err != nil {
		return Inventory{}, fmt.Errorf("inventory %q: %w", path, err)
	}
	return inventory, nil
}

func MarshalInventory(repo, foreign []string) ([]byte, error) {
	return MarshalFullInventory(Inventory{Repo: repo, Foreign: foreign})
}

// MarshalFullInventory serializes the complete optional inventory snapshot. The
// referenced file keeps package-manager and hardware-specific inventory out of
// the main configuration while still participating in effective state.
func MarshalFullInventory(inv Inventory) ([]byte, error) {
	cfg := Default()
	cfg.Reference.PackageFiles = []string{}
	cfg.Packages.Repo = SortedUnique(inv.Repo)
	cfg.Packages.Foreign.Items = SortedUnique(inv.Foreign)
	cfg.Flatpak.Pkgs = SortedUnique(inv.Flatpak)
	cfg.Flatpak.UserPkgs = SortedUnique(inv.FlatpakUser)
	cfg.Greeter.Pkgs = SortedUnique(inv.GreeterPkgs)
	cfg.Greeter.System, cfg.Greeter.Theme, cfg.Greeter.ThemeSource, cfg.Greeter.ThemePackage = inv.GreeterSystem, inv.GreeterTheme, inv.GreeterThemeSource, inv.GreeterThemePackage
	cfg.Mouse.Pkgs, cfg.Mouse.Theme, cfg.Mouse.ThemeSource, cfg.Mouse.ThemePackage, cfg.Mouse.Size = SortedUnique(inv.MousePkgs), inv.MouseTheme, inv.MouseThemeSource, inv.MouseThemePackage, inv.MouseSize
	cfg.Desktop.Pkgs, cfg.Desktop.Config = SortedUnique(inv.DesktopPkgs), SortedUnique(inv.DesktopConfig)
	cfg.Audio.Pkgs, cfg.Audio.Config = SortedUnique(inv.AudioPkgs), SortedUnique(inv.AudioConfig)
	cfg.Kernel.Pkgs, cfg.Kernel.Config = SortedUnique(inv.KernelPkgs), SortedUnique(inv.KernelConfig)
	cfg.Fonts.Pkgs = SortedUnique(inv.FontPkgs)
	cfg.Fonts.Config = SortedUnique(inv.FontConfig)
	cfg.Locale = LocaleConfig{SystemLanguage: inv.SystemLanguage, KeyboardLayout: inv.KeyboardLayout, KeyboardVariant: inv.KeyboardVariant, KeyboardModel: inv.KeyboardModel, KeyboardOptions: inv.KeyboardOptions}
	cfg.Python.UV.Pkgs = SortedUnique(inv.PythonUV)
	cfg.Python.UV.Config = SortedUnique(inv.PythonUVConfig)
	cfg.Python.Pip3.Pkgs = SortedUnique(inv.PythonPip3)
	cfg.Python.Pip3.Config = SortedUnique(inv.PythonPip3Config)
	cfg.Node.NPM.Pkgs = SortedUnique(inv.NodeNPM)
	cfg.Node.NPM.Config = SortedUnique(inv.NodeNPMConfig)
	cfg.Rust.Cargo.Pkgs = SortedUnique(inv.RustCargo)
	cfg.Rust.Cargo.Config = SortedUnique(inv.RustCargoConfig)
	cfg.Golang.Pkgs = SortedUnique(inv.Golang)
	cfg.Golang.Config = SortedUnique(inv.GolangConfig)
	if cfg.Greeter.ThemeSource == "" {
		cfg.Greeter.ThemeSource = "local"
	}
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	data, err := encodeInventoryTOML(cfg)
	if err != nil {
		return nil, err
	}
	return []byte(data), nil
}

func encodeInventoryTOML(cfg Config) (string, error) {
	var out strings.Builder
	fmt.Fprintf(&out, "# Refreshed by `zacman init -axp`; edit only if you want to preserve a custom inventory.\nschema_version = %d\n\n[packages]\nrepo = ", SchemaVersion)
	writeArray(&out, cfg.Packages.Repo, "  ")
	out.WriteString("\n\n[packages.foreign]\nitems = ")
	writeArray(&out, cfg.Packages.Foreign.Items, "  ")
	writePackageSection(&out, "kernel", cfg.Kernel)
	writePackageSection(&out, "audio", cfg.Audio)
	writeComponentTable(&out, "desktop", cfg.Desktop)
	writePackageSection(&out, "fonts", cfg.Fonts)
	out.WriteString("\n[flatpak]\npkgs = ")
	writeArray(&out, cfg.Flatpak.Pkgs, "  ")
	out.WriteString("\nuser_pkgs = ")
	writeArray(&out, cfg.Flatpak.UserPkgs, "  ")
	out.WriteString("\n\n[greeter]\npkgs = ")
	writeArray(&out, cfg.Greeter.Pkgs, "  ")
	fmt.Fprintf(&out, "\nsystem = %s\ntheme = %s\ntheme_source = %s\ntheme_package = %s\n", strconv.Quote(cfg.Greeter.System), strconv.Quote(cfg.Greeter.Theme), strconv.Quote(cfg.Greeter.ThemeSource), strconv.Quote(cfg.Greeter.ThemePackage))
	out.WriteString("\n[mouse]\npkgs = ")
	writeArray(&out, cfg.Mouse.Pkgs, "  ")
	fmt.Fprintf(&out, "\ntheme = %s\ntheme_source = %s\ntheme_package = %s\nsize = %d\n", strconv.Quote(cfg.Mouse.Theme), strconv.Quote(cfg.Mouse.ThemeSource), strconv.Quote(cfg.Mouse.ThemePackage), cfg.Mouse.Size)
	out.WriteString("\n[locale]\n")
	fmt.Fprintf(&out, "system_language = %s\nkeyboard_layout = %s\nkeyboard_variant = %s\nkeyboard_model = %s\nkeyboard_options = %s\n", strconv.Quote(cfg.Locale.SystemLanguage), strconv.Quote(cfg.Locale.KeyboardLayout), strconv.Quote(cfg.Locale.KeyboardVariant), strconv.Quote(cfg.Locale.KeyboardModel), strconv.Quote(cfg.Locale.KeyboardOptions))
	writePackageSection(&out, "python.uv", PackageSection{Pkgs: cfg.Python.UV.Pkgs, Config: cfg.Python.UV.Config})
	writePackageSection(&out, "python.pip3", PackageSection{Pkgs: cfg.Python.Pip3.Pkgs, Config: cfg.Python.Pip3.Config})
	writePackageSection(&out, "node.npm", PackageSection{Pkgs: cfg.Node.NPM.Pkgs, Config: cfg.Node.NPM.Config})
	writePackageSection(&out, "rust.cargo", PackageSection{Pkgs: cfg.Rust.Cargo.Pkgs, Config: cfg.Rust.Cargo.Config})
	writePackageSection(&out, "golang", PackageSection{Pkgs: cfg.Golang.Pkgs, Config: cfg.Golang.Config})
	return out.String(), nil
}

func WriteInventoryAtomic(path string, repo, foreign []string) error {
	return WriteFullInventoryAtomic(path, Inventory{Repo: repo, Foreign: foreign})
}

func WriteFullInventoryAtomic(path string, inv Inventory) error {
	data, err := MarshalFullInventory(inv)
	if err != nil {
		return err
	}
	return writeAtomicBytes(path, data)
}

// EnsureInventoryFile creates an empty inventory only when it does not exist.
// Existing user content is validated and preserved.
func EnsureInventoryFile(path string) error {
	if _, err := os.Lstat(path); err == nil {
		_, err := ReadInventory(path)
		return err
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect inventory path %q: %w", path, err)
	}
	parent := filepath.Dir(path)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect inventory directory %q: %w", parent, err)
	}
	if err := validateParentDirectory(parent, parentInfo); err != nil {
		return err
	}
	data, err := MarshalInventory(nil, nil)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			_, checkErr := ReadInventory(path)
			return checkErr
		}
		return fmt.Errorf("create inventory %q: %w", path, err)
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write inventory %q: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync inventory %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close inventory %q: %w", path, err)
	}
	ok = true
	return nil
}

// RemovePackagesFromReferences removes names from every referenced inventory.
// It is used by zacman uninstall so a package imported via -axp does not become
// desired again on the next reconciliation.
func RemovePackagesFromReferences(path string, packages []string) ([]string, error) {
	cfg, err := LoadBase(path)
	if err != nil {
		return nil, err
	}
	requested := make(map[string]struct{}, len(packages))
	for _, pkg := range packages {
		requested[pkg] = struct{}{}
	}
	removed := map[string]struct{}{}
	for _, name := range cfg.Reference.PackageFiles {
		inventoryPath := ReferenceFilePath(path, name)
		inventory, err := ReadInventory(inventoryPath)
		if err != nil {
			return SortedUnique(mapKeys(removed)), fmt.Errorf("read referenced inventory %q: %w", name, err)
		}
		keepRepo := make([]string, 0, len(inventory.Repo))
		for _, pkg := range inventory.Repo {
			if _, ok := requested[pkg]; ok {
				removed[pkg] = struct{}{}
				continue
			}
			keepRepo = append(keepRepo, pkg)
		}
		keepForeign := make([]string, 0, len(inventory.Foreign))
		for _, pkg := range inventory.Foreign {
			if _, ok := requested[pkg]; ok {
				removed[pkg] = struct{}{}
				continue
			}
			keepForeign = append(keepForeign, pkg)
		}
		if len(keepRepo) != len(inventory.Repo) || len(keepForeign) != len(inventory.Foreign) {
			if err := WriteInventoryAtomic(inventoryPath, keepRepo, keepForeign); err != nil {
				return SortedUnique(mapKeys(removed)), fmt.Errorf("update referenced inventory %q: %w", name, err)
			}
		}
	}
	return SortedUnique(mapKeys(removed)), nil
}

func mapKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	return out
}

func validateReferenceFiles(values []string) error {
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		if value == "" || filepath.Base(value) != value || strings.Contains(value, "..") || !strings.HasSuffix(value, ".toml") || strings.TrimSpace(value) != value {
			return fmt.Errorf("reference.package_files[%d]: expected a simple .toml filename in the Zacman config directory, got %q", i, value)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("reference.package_files: duplicate filename %q", value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateParentDirectory(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("refusing path with unsafe parent directory: %s", path)
	}
	if info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0 {
		return fmt.Errorf("config directory is group/world-writable: %s", path)
	}
	if os.Geteuid() == 0 && !ownedByRoot(info) && !ownedBySudoUser(info) {
		return fmt.Errorf("privileged config directory must be owned by root or the invoking sudo user: %s", path)
	}
	return nil
}

func validateConfigFile(path string, info os.FileInfo) error {
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("config file is group/world-writable: %s", path)
	}
	if os.Geteuid() == 0 && !ownedByRoot(info) && !ownedBySudoUser(info) {
		return fmt.Errorf("privileged config file must be owned by root or the invoking sudo user: %s", path)
	}
	return nil
}

func ownedByRoot(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0
}

func ownedBySudoUser(info os.FileInfo) bool {
	uid, _, ok := sudoTargetIDs()
	if !ok {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uid
}

func sudoTargetIDs() (uint32, uint32, bool) {
	uidText := strings.TrimSpace(os.Getenv("SUDO_UID"))
	if uidText == "" {
		return 0, 0, false
	}
	uidValue, err := strconv.ParseUint(uidText, 10, 32)
	if err != nil || uidValue == 0 {
		return 0, 0, false
	}
	gidText := strings.TrimSpace(os.Getenv("SUDO_GID"))
	var gidValue uint64
	if gidText != "" {
		gidValue, err = strconv.ParseUint(gidText, 10, 32)
		if err != nil {
			return 0, 0, false
		}
	} else {
		account, lookupErr := osuser.LookupId(uidText)
		if lookupErr != nil {
			return 0, 0, false
		}
		gidValue, err = strconv.ParseUint(account.Gid, 10, 32)
		if err != nil {
			return 0, 0, false
		}
	}
	return uint32(uidValue), uint32(gidValue), true
}

// FixOwnership makes the directory containing the specified manifest and all
// regular config/inventory/profile files user-editable for the invoking sudo
// user. It rejects symlinks and special files, never follows links, and removes
// group/world-write permissions before transferring ownership.
func FixOwnership(manifestPath string) error {
	if os.Geteuid() != 0 {
		return errors.New("fixing /etc/zacman ownership requires root; run 'sudo zacman doctor'")
	}
	uid, gid, ok := sudoTargetIDs()
	if !ok {
		return errors.New("cannot determine the invoking user; run Zacman through sudo from your normal user session")
	}
	dir := filepath.Dir(normalizeConfigPath(manifestPath))
	rootInfo, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("inspect config directory %s: %w", dir, err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("refusing unsafe config directory: %s", dir)
	}
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to change ownership through symlink %s", path)
		}
		mode := info.Mode().Perm() &^ 0022
		switch {
		case info.IsDir():
			mode |= 0700 // owner can list, edit, and create config files
		case info.Mode().IsRegular():
			mode |= 0600 // owner can edit config/inventory files
		default:
			return fmt.Errorf("refusing to change ownership of special file %s", path)
		}
		if err := os.Chown(path, int(uid), int(gid)); err != nil {
			return fmt.Errorf("change ownership of %s: %w", path, err)
		}
		if err := os.Chmod(path, mode); err != nil {
			return fmt.Errorf("secure permissions on %s: %w", path, err)
		}
		return nil
	})
}

func SortedUnique(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func Contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// ValidatePackages validates a CLI-supplied package list independently of the manifest.
// ValidateServiceUnit validates a systemd system-unit name before it is
// written to the manifest or passed as an argument to systemctl.
func ValidateServiceUnit(unit string) error {
	if strings.TrimSpace(unit) != unit || unit == "" || !unitNameRE.MatchString(unit) {
		return fmt.Errorf("invalid systemd unit name %q (include a supported suffix such as .service)", unit)
	}
	return nil
}

func ValidatePackages(values []string) error {
	return uniqueAndValid("packages", values, packageNameRE)
}
