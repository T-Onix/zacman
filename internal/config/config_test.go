package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const validTOML = `schema_version = 1

[packages]
repo = [
  "git",
  "neovim", # inline comment
]

ignore_updates = []

[packages.foreign]
items = ["custom-local-package"]

[packages.aur]
enabled = false
items = []

[services]
enable = ["NetworkManager.service"]
disable = ['cups.service']

[mirrors]
servers = ["https://geo.mirror.pkgbuild.com/$repo/os/$arch"]
blackarch_servers = ["https://mirror.cyberbits.eu/blackarch/$repo/os/$arch"]

[pacman]
ilove_candy = true
`

func TestDefaultConfigStartsEmptyAndEnablesCandy(t *testing.T) {
	cfg := Default()
	if len(cfg.Packages.Repo) != 0 {
		t.Fatalf("default repo packages must be empty, got %#v", cfg.Packages.Repo)
	}
	if len(cfg.Packages.IgnoreUpdates) != 0 || len(cfg.Mirrors.Servers) != 0 || len(cfg.Mirrors.BlackarchServers) != 0 {
		t.Fatalf("default exclusions and mirrors must be empty: %#v", cfg)
	}
	if !cfg.Pacman.ILoveCandy {
		t.Fatal("I Love Candy must be enabled by default")
	}
	if len(cfg.Reference.PackageFiles) != 0 {
		t.Fatalf("fresh configs must not reference generated package inventory files: %#v", cfg.Reference.PackageFiles)
	}
}

func TestAURItemsAreInertWhenDisabled(t *testing.T) {
	raw := strings.Replace(validTOML, "[packages.aur]\nenabled = false\nitems = []", "[packages.aur]\nenabled = false\nitems = [\"paru\", \"yay\"]", 1)
	cfg, err := Decode([]byte(raw))
	if err != nil {
		t.Fatalf("disabled AUR notes should be valid: %v", err)
	}
	if !reflect.DeepEqual(cfg.Packages.AUR.Items, []string{"paru", "yay"}) {
		t.Fatalf("AUR notes not preserved: %#v", cfg.Packages.AUR.Items)
	}
	cfg.Packages.AUR.Enabled = true
	if err := Validate(cfg); err != nil {
		t.Fatalf("enabled AUR backend with valid package names should validate: %v", err)
	}
}

func TestExampleConfigParsesAndReferencesOnlyExistingInventory(t *testing.T) {
	data, err := os.ReadFile("../../examples/zacman.toml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Decode(data)
	if err != nil {
		t.Fatalf("example config is invalid TOML: %v", err)
	}
	if len(cfg.Reference.PackageFiles) != 0 {
		t.Fatalf("example must not reference generated inventory files by default: %#v", cfg.Reference.PackageFiles)
	}
	if len(cfg.Packages.Repo) != 0 {
		t.Fatalf("example must not force default packages: %#v", cfg.Packages.Repo)
	}
	if cfg.Packages.AUR.Enabled {
		t.Fatal("AUR backend must remain disabled")
	}
}

func TestMarshalDefaultHasAURExamplesButNoInventoryOrPacmanTemplate(t *testing.T) {
	data, err := Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"# \"paru\",", "# \"yay\","} {
		if !strings.Contains(text, want) {
			t.Errorf("default config example missing %q", want)
		}
	}
	for _, forbidden := range []string{"existing-pkgs.toml", "pacman.conf.example", "Pacman config references only", "ParallelDownloads = 5", "[multilib]"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("fresh Zacman config must not contain generated inventory/Pacman template text %q", forbidden)
		}
	}
	if _, err := Decode(data); err != nil {
		t.Fatalf("generated default TOML should parse: %v", err)
	}
}

func TestDecodeValidConfig(t *testing.T) {
	cfg, err := Decode([]byte(validTOML))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if cfg.SchemaVersion != 1 {
		t.Fatalf("schema version = %d", cfg.SchemaVersion)
	}
	if len(cfg.Packages.Repo) != 2 || cfg.Packages.Repo[0] != "git" {
		t.Fatalf("unexpected repo list: %#v", cfg.Packages.Repo)
	}
	if len(cfg.Packages.Foreign.Items) != 1 || cfg.Packages.Foreign.Items[0] != "custom-local-package" {
		t.Fatalf("unexpected foreign inventory: %#v", cfg.Packages.Foreign.Items)
	}
	if cfg.Services.Enable[0] != "NetworkManager.service" || cfg.Services.Disable[0] != "cups.service" {
		t.Fatalf("unexpected services: %#v", cfg.Services)
	}
	if !cfg.Pacman.ILoveCandy {
		t.Fatal("ILoveCandy should be enabled in the sample config")
	}
	if len(cfg.Mirrors.Servers) != 1 || len(cfg.Mirrors.BlackarchServers) != 1 {
		t.Fatalf("unexpected mirror settings: %#v", cfg.Mirrors)
	}
}

func TestDecodeRejectsUnknownFieldsAndInvalidSchema(t *testing.T) {
	cases := []string{
		"schema_version = 1\nunknown = true\n",
		"schema_version = 1\n",
		"[packages]\nrepo = []\n[packages.aur]\nenabled = false\nitems = []\n[services]\nenable = []\ndisable = []\n",
		"schema_version = 2\n",
		"schema_version = 1\n[packages]\nrepo = [\"git\", \"git\"]\n",
		"schema_version = 1\n[packages]\nrepo = [\".\"]\n",
		"schema_version = 1\n[packages.aur]\nenabled = true\nitems = []\n",
		"schema_version = 1\n[services]\nenable = [\"same.service\"]\ndisable = [\"same.service\"]\n",
		"schema_version = 1\n[packages]\nrepo = [not-a-string]\n",
		"schema_version = 1\n[packages]\nrepo = [\"git\"]\nrepo = [\"vim\"]\n",
		"schema_version = 1\n[packages]\nrepo = [\"git\"]\n[packages.foreign]\nitems = [\"git\"]\n",
		"schema_version = 1\n[packages]\nrepo = []\nignore_updates = [\"bad package\"]\n",
		"schema_version = 1\n[mirrors]\nservers = [\"file:///etc/passwd\"]\n",
		"schema_version = 1\n[mirrors]\nblackarch_servers = [\"rsync://mirror.example/blackarch\"]\n",
		"schema_version = 1\n[not_real]\n",
		"schema_version = 1\n[reference]\npackage_files = [\"../outside.toml\"]\n[packages]\nrepo = []\n[packages.aur]\nenabled = false\nitems = []\n[services]\nenable = []\ndisable = []\n",
	}
	for i, raw := range cases {
		if _, err := Decode([]byte(raw)); err == nil {
			t.Errorf("case %d: expected error for %q", i, raw)
		}
	}
}

func TestDecodeRejectsMalformedTOML(t *testing.T) {
	for _, raw := range []string{
		"schema_version = 1\n[packages\n",
		"schema_version = 1\n[packages]\nrepo = [\"git\"\n",
		"schema_version = 1\n[packages]\nrepo = [\"git\",,]\n",
		"schema_version = 1\n[packages]\nrepo = [\"bad\\x20name\"]\n",
		"schema_version = 01\n",
		"schema_version = 1\n[packages]\nrepo = [\"git\" ",
	} {
		if _, err := Decode([]byte(raw)); err == nil {
			t.Errorf("expected malformed TOML rejection: %q", raw)
		}
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	cfg, err := Decode([]byte(validTOML))
	if err != nil {
		t.Fatal(err)
	}
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Decode(data)
	if err != nil {
		t.Fatalf("round-trip decode: %v\n%s", err, data)
	}
	if strings.Join(again.Packages.Repo, ",") != strings.Join(cfg.Packages.Repo, ",") {
		t.Fatalf("round trip changed packages: %#v", again.Packages.Repo)
	}
	if !again.Pacman.ILoveCandy || len(again.Packages.IgnoreUpdates) != 0 || len(again.Mirrors.Servers) != 1 || len(again.Mirrors.BlackarchServers) != 1 || len(again.Packages.Foreign.Items) != 1 || again.Packages.Foreign.Items[0] != "custom-local-package" {
		t.Fatalf("round trip changed new settings: %#v", again)
	}
}

func TestLoadExpandsReferencedInventoriesWithoutInliningThemOnDisk(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "zacman.toml")
	cfg := Default()
	cfg.Reference.PackageFiles = []string{"existing-pkgs.toml", "user-packages.toml"}
	cfg.Packages.Repo = []string{"wanted-but-not-installed"}
	if err := WriteAtomic(mainPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := WriteInventoryAtomic(filepath.Join(dir, "existing-pkgs.toml"), []string{"base", "git", "neovim"}, []string{"local-tool"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteInventoryAtomic(filepath.Join(dir, "user-packages.toml"), []string{"ripgrep", "git"}, nil); err != nil {
		t.Fatal(err)
	}

	base, err := LoadBase(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(base.Packages.Repo, []string{"wanted-but-not-installed"}) {
		t.Fatalf("LoadBase should only read the main manifest, got %#v", base.Packages.Repo)
	}

	got, err := Load(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	wantRepo := []string{"base", "git", "neovim", "ripgrep", "wanted-but-not-installed"}
	if !reflect.DeepEqual(got.Packages.Repo, wantRepo) {
		t.Fatalf("effective repo packages = %#v, want %#v", got.Packages.Repo, wantRepo)
	}
	if !reflect.DeepEqual(got.Packages.Foreign.Items, []string{"local-tool"}) {
		t.Fatalf("effective foreign packages = %#v", got.Packages.Foreign.Items)
	}

	raw, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"neovim", "ripgrep", "local-tool"} {
		if strings.Contains(string(raw), pkg) {
			t.Fatalf("referenced package %q was inlined into main manifest:\n%s", pkg, raw)
		}
	}
	if !strings.Contains(string(raw), "existing-pkgs.toml") || !strings.Contains(string(raw), "user-packages.toml") {
		t.Fatalf("main manifest doesn't reference both inventory files:\n%s", raw)
	}
}

func TestDecodeInventoryOnlyAcceptsPackageFields(t *testing.T) {
	valid := []byte(`schema_version = 1

[packages]
repo = ["git", "neovim"]

[packages.foreign]
items = ["local-tool"]
`)
	got, err := DecodeInventory(valid)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Repo, []string{"git", "neovim"}) || !reflect.DeepEqual(got.Foreign, []string{"local-tool"}) {
		t.Fatalf("unexpected inventory: %#v", got)
	}
	bad := []byte(`schema_version = 1

[packages]
repo = []

[services]
enable = ["sshd.service"]
`)
	if _, err := DecodeInventory(bad); err == nil {
		t.Fatal("inventory must not accept service settings")
	}
}

func TestInventoryRoundTripHandlesThousandsOfPackages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing-pkgs.toml")
	packages := make([]string, 2000)
	for i := range packages {
		packages[i] = fmt.Sprintf("pkg%04d", i)
	}
	if err := WriteInventoryAtomic(path, packages, nil); err != nil {
		t.Fatal(err)
	}
	got, err := ReadInventory(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repo) != len(packages) {
		t.Fatalf("decoded %d packages, want %d", len(got.Repo), len(packages))
	}
	if got.Repo[0] != "pkg0000" || got.Repo[len(got.Repo)-1] != "pkg1999" {
		t.Fatalf("inventory is not sorted deterministically: first=%q last=%q", got.Repo[0], got.Repo[len(got.Repo)-1])
	}
}

func TestEnsureInventoryPreservesExistingManualFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user-packages.toml")
	if err := WriteInventoryAtomic(path, []string{"custom-tool"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := EnsureInventoryFile(path); err != nil {
		t.Fatal(err)
	}
	got, err := ReadInventory(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Repo, []string{"custom-tool"}) {
		t.Fatalf("EnsureInventoryFile overwrote manual content: %#v", got.Repo)
	}
}

func TestRemovePackagesFromReferencesRemovesFromEveryFile(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "zacman.toml")
	cfg := Default()
	cfg.Reference.PackageFiles = []string{"existing-pkgs.toml", "user-packages.toml"}
	if err := WriteAtomic(mainPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := WriteInventoryAtomic(filepath.Join(dir, "existing-pkgs.toml"), []string{"git", "neovim"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := WriteInventoryAtomic(filepath.Join(dir, "user-packages.toml"), []string{"neovim", "ripgrep"}, nil); err != nil {
		t.Fatal(err)
	}
	removed, err := RemovePackagesFromReferences(mainPath, []string{"neovim"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(removed, []string{"neovim"}) {
		t.Fatalf("removed = %#v", removed)
	}
	got, err := Load(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(got.Packages.Repo, []string{"git", "neovim", "ripgrep"}) || Contains(got.Packages.Repo, "neovim") {
		t.Fatalf("neovim remains in effective package list: %#v", got.Packages.Repo)
	}
}

func TestWriteAtomicRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.toml")
	if err := os.WriteFile(target, []byte(validTOML), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(link, Default()); err == nil {
		t.Fatal("expected symlink refusal")
	}
}

func TestExtendedEcosystemAndSystemSectionsRoundTrip(t *testing.T) {
	raw := validTOML + `
[flatpak]
pkgs = ["org.gimp.GIMP"]
user_pkgs = ["com.spotify.Client"]
remote = "flathub"
scope = "system"

[greeter]
pkgs = ["sddm"]
system = "sddm"
theme = "breeze"
theme_source = "repo"
theme_package = "sddm-theme-breeze"

[desktop]
pkgs = ["niri"]
config = ["profiles/niri/config.kdl=>~/.config/niri/config.kdl"]

[audio]
pkgs = ["pipewire", "wireplumber"]
config = []

[kernel]
pkgs = ["linux", "linux-firmware"]
config = []

[fonts]
pkgs = ["ttf-jetbrains-mono-nerd"]
config = []

[locale]
system_language = "en_US.UTF-8"
keyboard_layout = "us"

[python.uv]
pkgs = ["ruff"]
config = []

[python.pip3]
pkgs = ["httpx"]
config = []

[node.npm]
pkgs = ["eslint"]
config = []

[rust.cargo]
pkgs = ["bat"]
config = []

[golang]
pkgs = ["github.com/example/tool@latest"]
config = []
`
	cfg, err := Decode([]byte(raw))
	if err != nil {
		t.Fatalf("decode extended configuration: %v", err)
	}
	if len(cfg.Flatpak.Pkgs) != 1 || cfg.Flatpak.Pkgs[0] != "org.gimp.GIMP" || len(cfg.Flatpak.UserPkgs) != 1 {
		t.Fatalf("flatpak values not decoded: %#v", cfg.Flatpak)
	}
	if cfg.Greeter.System != "sddm" || cfg.Greeter.Theme != "breeze" || cfg.Greeter.ThemePackage != "sddm-theme-breeze" {
		t.Fatalf("greeter fields not decoded: %#v", cfg.Greeter)
	}
	if cfg.Locale.SystemLanguage != "en_US.UTF-8" || cfg.Locale.KeyboardLayout != "us" {
		t.Fatalf("locale fields not decoded: %#v", cfg.Locale)
	}
	if len(cfg.Python.UV.Pkgs) != 1 || cfg.Python.UV.Pkgs[0] != "ruff" || cfg.Node.NPM.Pkgs[0] != "eslint" || cfg.Rust.Cargo.Pkgs[0] != "bat" || cfg.Golang.Pkgs[0] != "github.com/example/tool@latest" {
		t.Fatalf("ecosystem packages not decoded: %#v", cfg)
	}

	data, err := Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal extended config: %v", err)
	}
	again, err := Decode(data)
	if err != nil {
		t.Fatalf("decode serialized extended config: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(again.Desktop, cfg.Desktop) || !reflect.DeepEqual(again.Audio, cfg.Audio) || !reflect.DeepEqual(again.Kernel, cfg.Kernel) || !reflect.DeepEqual(again.Fonts, cfg.Fonts) {
		t.Fatalf("component sections changed on roundtrip:\nwant %#v\ngot %#v", cfg, again)
	}
	for _, pkg := range []string{"sddm", "sddm-theme-breeze", "niri", "pipewire", "linux", "linux-firmware", "ttf-jetbrains-mono-nerd", "git", "neovim"} {
		if !Contains(AllPacmanPackages(again), pkg) {
			t.Errorf("AllPacmanPackages missing %q: %#v", pkg, AllPacmanPackages(again))
		}
	}
}

func TestCategoryWarningsRecommendDedicatedSections(t *testing.T) {
	cfg := Default()
	cfg.Packages.Repo = []string{"linux", "pipewire", "sddm", "ttf-jetbrains-mono-nerd", "org.gimp.GIMP", "com.spotify.Client", "io.github.someapp", "git", "alsa-utils", "xfce4", "freetype2", "sddm-theme-breeze"}
	warnings := CategoryWarnings(cfg)
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"linux", "kernel", "pipewire", "audio", "sddm", "greeter", "ttf-jetbrains-mono-nerd", "fonts", "org.gimp.GIMP", "com.spotify.Client", "io.github.someapp", "[flatpak].pkgs", "alsa-utils", "xfce4", "freetype2", "sddm-theme-breeze"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "git looks like") {
		t.Fatalf("ordinary repo package should not warn: %s", joined)
	}
}

func TestFullInventoryPreservesExternalManagersAndSystemSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing-pkgs.toml")
	inv := Inventory{
		Repo: []string{"git", "niri"}, Foreign: []string{"local-package"},
		Flatpak: []string{"org.gimp.GIMP"}, FlatpakUser: []string{"com.spotify.Client"},
		GreeterPkgs: []string{"sddm"}, GreeterSystem: "sddm", GreeterTheme: "breeze", GreeterThemeSource: "local",
		DesktopPkgs: []string{"niri"}, DesktopConfig: []string{"profiles/niri/config.kdl=>~/.config/niri/config.kdl"},
		AudioPkgs: []string{"pipewire"}, KernelPkgs: []string{"linux"}, FontPkgs: []string{"ttf-jetbrains-mono-nerd"},
		SystemLanguage: "en_US.UTF-8", KeyboardLayout: "us", PythonUV: []string{"ruff"}, PythonPip3: []string{"httpx"},
		NodeNPM: []string{"eslint"}, RustCargo: []string{"bat"}, Golang: []string{"github.com/example/tool@v1.2.3"},
	}
	if err := WriteFullInventoryAtomic(path, inv); err != nil {
		t.Fatalf("write inventory: %v", err)
	}
	got, err := ReadInventory(path)
	if err != nil {
		t.Fatalf("read inventory: %v", err)
	}
	if !reflect.DeepEqual(got.Repo, SortedUnique(inv.Repo)) || !reflect.DeepEqual(got.Foreign, inv.Foreign) || !reflect.DeepEqual(got.Flatpak, inv.Flatpak) || !reflect.DeepEqual(got.FlatpakUser, inv.FlatpakUser) {
		t.Fatalf("package inventory didn't roundtrip: want %#v got %#v", inv, got)
	}
	if got.GreeterSystem != "sddm" || got.GreeterTheme != "breeze" || got.SystemLanguage != "en_US.UTF-8" || got.KeyboardLayout != "us" {
		t.Fatalf("system preferences didn't roundtrip: %#v", got)
	}
	if !reflect.DeepEqual(got.PythonUV, inv.PythonUV) || !reflect.DeepEqual(got.PythonPip3, inv.PythonPip3) || !reflect.DeepEqual(got.NodeNPM, inv.NodeNPM) || !reflect.DeepEqual(got.RustCargo, inv.RustCargo) || !reflect.DeepEqual(got.Golang, inv.Golang) {
		t.Fatalf("external manager inventory didn't roundtrip: %#v", got)
	}
}

func TestLoadDoesNotRestoreStaleSDDMThemeWhenGreeterChanges(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "zacman.toml")
	cfg := Default()
	cfg.Reference.PackageFiles = []string{"existing-pkgs.toml"}
	cfg.Greeter.System = "gdm"
	if err := WriteAtomic(mainPath, cfg); err != nil {
		t.Fatal(err)
	}
	inv := Inventory{
		Repo: []string{"gdm"}, Foreign: []string{},
		GreeterPkgs: []string{"sddm"}, GreeterSystem: "sddm",
		GreeterTheme: "breeze", GreeterThemeSource: "local",
	}
	if err := WriteFullInventoryAtomic(filepath.Join(dir, "existing-pkgs.toml"), inv); err != nil {
		t.Fatal(err)
	}
	got, err := Load(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Greeter.System != "gdm" {
		t.Fatalf("explicit greeter changed to %q", got.Greeter.System)
	}
	if got.Greeter.Theme != "" || got.Greeter.ThemeSource != "" || got.Greeter.ThemePackage != "" {
		t.Fatalf("stale SDDM theme metadata was restored for GDM: %#v", got.Greeter)
	}
}

func TestSpecializedPacmanPackagesAndThemePathsAreValidated(t *testing.T) {
	cfg := Default()
	cfg.Kernel.Pkgs = []string{"linux"}
	if err := Validate(cfg); err != nil {
		t.Fatalf("valid kernel package rejected: %v", err)
	}
	cfg.Kernel.Pkgs = []string{"foo=bar"}
	if err := Validate(cfg); err == nil {
		t.Fatal("Pacman component list must reject package targets containing operators")
	}
	cfg = Default()
	cfg.Greeter.System = "sddm"
	cfg.Greeter.Theme = "../../etc/passwd"
	if err := Validate(cfg); err == nil {
		t.Fatal("theme path traversal should be rejected")
	}
	cfg.Greeter.Theme = "breeze-dark"
	if err := Validate(cfg); err != nil {
		t.Fatalf("valid theme name rejected: %v", err)
	}
	cfg.Greeter.Theme = "/usr/share/sddm/themes/where-is-my-sddm-theme"
	cfg.Greeter.ThemeSource = "local"
	if err := Validate(cfg); err != nil {
		t.Fatalf("valid absolute SDDM theme path rejected: %v", err)
	}
	name, dir, err := ResolveSDDMTheme(cfg.Greeter.Theme)
	if err != nil {
		t.Fatalf("resolve absolute theme path: %v", err)
	}
	if name != "where-is-my-sddm-theme" || dir != "/usr/share/sddm/themes/where-is-my-sddm-theme" {
		t.Fatalf("unexpected resolved theme: name=%q dir=%q", name, dir)
	}
	cfg.Greeter.Theme = "/etc/passwd"
	if err := Validate(cfg); err == nil {
		t.Fatal("absolute theme paths outside SDDM theme root should be rejected")
	}
	cfg.Greeter.Theme = "/usr/share/sddm/themes/../breeze"
	if err := Validate(cfg); err == nil {
		t.Fatal("theme paths that escape SDDM theme root should be rejected")
	}
}

func TestWriteAtomicPreservesRepoCommentsAndTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zacman.toml")
	original := strings.Replace(validTOML, "repo = [\n  \"git\",\n  \"neovim\", # inline comment\n]", "# Keep my repo notes\nrepo = [\n  # This comment describes git\n  \"git\", # git is important\n  \"neovim\", # inline comment\n]", 1)
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadBase(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Packages.Repo = SortedUnique(append(cfg.Packages.Repo, "fastfetch"))
	if err := WriteAtomic(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, comment := range []string{"# Keep my repo notes", "# This comment describes git", "# git is important", "# inline comment"} {
		if !strings.Contains(text, comment) {
			t.Errorf("comment %q was lost after WriteAtomic:\n%s", comment, text)
		}
	}
	if !strings.HasSuffix(text, "\n") {
		t.Fatal("WriteAtomic should preserve the original trailing newline")
	}
	if _, err := Decode(got); err != nil {
		t.Fatalf("rewritten config is invalid TOML: %v\n%s", err, text)
	}
}

func TestOrganizePackagesMovesRecognizedPackagesToSpecializedSections(t *testing.T) {
	cfg := Default()
	cfg.Packages.Repo = []string{"git", "ttf-jetbrains-mono-nerd", "pipewire", "linux", "sddm", "org.gimp.GIMP"}
	got, moved := OrganizePackages(cfg)
	if !reflect.DeepEqual(got.Packages.Repo, []string{"git"}) {
		t.Fatalf("repo packages after organization = %#v", got.Packages.Repo)
	}
	for field, values := range map[string][]string{
		"fonts": got.Fonts.Pkgs, "audio": got.Audio.Pkgs, "kernel": got.Kernel.Pkgs,
		"greeter": got.Greeter.Pkgs, "flatpak": got.Flatpak.Pkgs,
	} {
		if len(values) != 1 {
			t.Errorf("%s packages = %#v, want one", field, values)
		}
		if len(moved[field]) == 0 {
			t.Errorf("moved report missing category %s: %#v", field, moved)
		}
	}
}

func TestValidateParentDirectoryRejectsGroupWritable(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o775); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateParentDirectory(dir, info); err == nil {
		t.Fatal("group-writable config directory should be rejected")
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err = os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateParentDirectory(dir, info); err != nil {
		t.Fatalf("safe directory should pass validation: %v", err)
	}
}

func TestMouseCursorConfigRoundTripAndPackagePlanning(t *testing.T) {
	cfg := Default()
	cfg.Mouse = MouseConfig{
		Pkgs:         []string{"xcursor-breeze"},
		Theme:        "Breeze_Light",
		ThemeSource:  "repo",
		ThemePackage: "xcursor-breeze",
		Size:         32,
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("valid mouse cursor config rejected: %v", err)
	}
	for _, want := range []string{"xcursor-breeze"} {
		if !Contains(AllPacmanPackages(cfg), want) {
			t.Errorf("AllPacmanPackages missing mouse package %q", want)
		}
	}
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("round trip: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(got.Mouse, cfg.Mouse) {
		t.Fatalf("mouse settings round trip mismatch: want %#v got %#v", cfg.Mouse, got.Mouse)
	}
}

func TestResolveCursorThemeAcceptsStandardPathsAndRejectsTraversal(t *testing.T) {
	for _, path := range []string{
		"/usr/share/icons/Breeze_Light",
		"/usr/local/share/icons/Custom",
		"/usr/share/icons/../icons/Breeze_Light",
	} {
		_, _, err := ResolveCursorTheme(path)
		if path == "/usr/share/icons/../icons/Breeze_Light" {
			if err == nil {
				t.Errorf("non-canonical path %q should be rejected", path)
			}
		} else if err != nil {
			t.Errorf("valid cursor path %q rejected: %v", path, err)
		}
	}
	for _, path := range []string{"/etc/passwd", "../../etc/passwd", ""} {
		if _, _, err := ResolveCursorTheme(path); err == nil {
			t.Errorf("unsafe cursor reference %q was accepted", path)
		}
	}
	if name, _, err := ResolveCursorTheme("Breeze_Light"); err != nil || name != "Breeze_Light" {
		t.Fatalf("valid cursor theme name resolution failed: name=%q err=%v", name, err)
	}
}

func TestMouseThemeSourceRequiresPackageForRepoThemes(t *testing.T) {
	cfg := Default()
	cfg.Mouse.Theme = "Breeze_Light"
	cfg.Mouse.ThemeSource = "repo"
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "mouse.theme_package") {
		t.Fatalf("expected mouse.theme_package validation error, got %v", err)
	}
	cfg.Mouse.ThemePackage = "xcursor-breeze"
	if err := Validate(cfg); err != nil {
		t.Fatalf("valid repo mouse theme rejected: %v", err)
	}
	cfg.Mouse.Size = 256
	if err := Validate(cfg); err == nil {
		t.Fatal("out-of-range cursor size should be rejected")
	}
}

func TestExampleConfigDecodes(t *testing.T) {
	data, err := os.ReadFile("../../examples/zacman.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(data); err != nil {
		t.Fatalf("example configuration is invalid: %v", err)
	}
}

func TestRepairDuplicatePackageEntriesPreservesCommentsAndFirstOccurrence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zacman.toml")
	raw := strings.Replace(validTOML, "repo = [\n  \"git\",\n  \"neovim\", # inline comment\n]", `repo = ["git", "neovim", "git"] # keep this note`, 1)
	raw = strings.Replace(raw, "items = []", `items = ["yay", "yay"] # AUR note`, 1)
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	duplicates, err := DuplicatePackageEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(duplicates, []string{"packages.repo: git", "packages.aur.items: yay"}) {
		t.Fatalf("duplicates = %#v", duplicates)
	}
	removed, err := RepairDuplicatePackageEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(removed, duplicates) {
		t.Fatalf("removed = %#v, want %#v", removed, duplicates)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{"# keep this note", "# AUR note"} {
		if !strings.Contains(text, want) {
			t.Fatalf("repaired config did not preserve comment %q:\n%s", want, text)
		}
	}
	repaired, err := LoadBase(path)
	if err != nil {
		t.Fatalf("repaired config should validate: %v", err)
	}
	if !reflect.DeepEqual(repaired.Packages.Repo, []string{"git", "neovim"}) || !reflect.DeepEqual(repaired.Packages.AUR.Items, []string{"yay"}) {
		t.Fatalf("duplicate values were not removed correctly: repo=%#v aur=%#v", repaired.Packages.Repo, repaired.Packages.AUR.Items)
	}
}
