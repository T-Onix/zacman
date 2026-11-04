package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/T-Onix/zacman/internal/config"
	"github.com/T-Onix/zacman/internal/pacman"
	"github.com/T-Onix/zacman/internal/planner"
)

func TestInstallDontUpdateFlagAcceptsEitherSideOfPackages(t *testing.T) {
	for _, input := range [][]string{{"-du", "neovim"}, {"neovim", "-du"}} {
		root := New("test").newCobraRoot()
		cmd, _, err := root.Find([]string{"install"})
		if err != nil {
			t.Fatalf("find install command: %v", err)
		}
		args := normalizeLegacyInitFlags(input)
		if err := cmd.Flags().Parse(args); err != nil {
			t.Fatalf("parse args %#v: %v", input, err)
		}
		got, err := cmd.Flags().GetBool("dont-update")
		if err != nil || !got {
			t.Fatalf("-du not recognized for %#v: value=%t err=%v", input, got, err)
		}
		if !reflect.DeepEqual(cmd.Flags().Args(), []string{"neovim"}) {
			t.Fatalf("package args for %#v = %#v", input, cmd.Flags().Args())
		}
	}
}

func TestNormalizeLegacyInitFlags(t *testing.T) {
	input := []string{"init", "-dc", "-du", "--dry-run"}
	got := normalizeLegacyInitFlags(input)
	want := []string{"init", "--dedupe-config", "--dont-update", "--dry-run"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized flags = %#v, want %#v", got, want)
	}
}

func TestParseOptionsAcceptsConfigBeforeAndAfterCommand(t *testing.T) {
	got, err := parseOptions([]string{"--config", "/tmp/zacman.toml", "diff"})
	if err != nil {
		t.Fatal(err)
	}
	if got.configPath != "/tmp/zacman.toml" || !reflect.DeepEqual(got.args, []string{"diff"}) {
		t.Fatalf("unexpected options: %#v", got)
	}

	got, err = parseOptions([]string{"diff", "--config=/tmp/other.toml", "--no-color"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.noColor {
		t.Fatal("expected --no-color to be recorded")
	}
	if got.configPath != "/tmp/other.toml" || !reflect.DeepEqual(got.args, []string{"diff"}) {
		t.Fatalf("unexpected options: %#v", got)
	}
}

func TestParseOptionsRejectsMissingConfigPath(t *testing.T) {
	if _, err := parseOptions([]string{"diff", "--config"}); err == nil {
		t.Fatal("expected missing path error")
	}
}

func TestShouldPassthroughAllPacmanSwitches(t *testing.T) {
	cases := [][]string{
		{"-S", "neovim"},
		{"-Ss", "neovim"},
		{"-Qi", "pacman"},
		{"-Rns", "package"},
		{"-U", "./package.pkg.tar.zst"},
		{"--config", "/etc/pacman.conf", "-Ss", "blackarch"},
		{"pacman", "--help"},
	}
	for _, args := range cases {
		if !shouldPassthroughToPacman(args) {
			t.Errorf("shouldPassthroughToPacman(%#v) = false", args)
		}
	}
}

func TestRunForwardsPacmanArgsVerbatim(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "pacman")
	capture := filepath.Join(dir, "captured-args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$PACMAN_CAPTURE\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACMAN_CAPTURE", capture)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := New("test").Run([]string{"-Ss", "some package", "--color", "never"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	want := "-Ss\nsome package\n--color\nnever\n"
	if string(got) != want {
		t.Fatalf("forwarded args = %q, want %q", got, want)
	}
}

func TestZacmanCommandsAreNotPacmanPassthrough(t *testing.T) {
	cases := [][]string{
		{"init"},
		{"--config", "/tmp/zacman.toml", "diff"},
		{"install", "git"},
		{"--help"},
		{"--version"},
	}
	for _, args := range cases {
		if shouldPassthroughToPacman(args) {
			t.Errorf("shouldPassthroughToPacman(%#v) = true", args)
		}
	}
}

func TestPlanCommandHasBeenRemoved(t *testing.T) {
	err := New("test").Run([]string{"plan"})
	if err == nil || !strings.Contains(err.Error(), "unknown Zacman command") {
		t.Fatalf("expected removed plan command error, got %v", err)
	}
}

func TestUnsupportedInstalledPackagesDoesNotClassifyAsAUR(t *testing.T) {
	cfg := config.Default()
	cfg.Packages.Repo = []string{"git", "local-tool"}
	got := unsupportedInstalledPackages(cfg, []string{"local-tool"})
	want := []string{"local-tool is currently installed as a foreign package, not a sync-repository package"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestCriticalUnitsAreWarnedAbout(t *testing.T) {
	for _, unit := range []string{"NetworkManager.service", "sshd.service", "systemd-logind.service"} {
		if !isCriticalUnit(unit) {
			t.Errorf("expected %s to be critical", unit)
		}
	}
	if isCriticalUnit("cups.service") {
		t.Fatal("cups.service should not be treated as a core critical unit")
	}
}

func TestConfirmDefaultsBlankLineToYes(t *testing.T) {
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin; _ = r.Close() }()
	confirmed, err := confirm("")
	if err != nil {
		t.Fatal(err)
	}
	if !confirmed {
		t.Fatal("blank line should accept the default action")
	}
}

func TestApplyCommandHasBeenRemoved(t *testing.T) {
	err := New("test").Run([]string{"apply"})
	if err == nil {
		t.Fatal("expected apply to be an unknown command")
	}
	if !strings.Contains(err.Error(), "unknown Zacman command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiffDoesNotPrintAlreadyInstalledPackageNames(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	printPlan(planner.Plan{AlreadyInstalled: []string{"base", "git", "fastfetch"}}, servicePlan{}, false, nil, nil)
	_ = w.Close()
	os.Stdout = oldStdout
	data, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	output := string(data)
	for _, pkg := range []string{"base", "git", "fastfetch"} {
		if strings.Contains(output, "\n  = "+pkg+"\n") {
			t.Fatalf("already-installed package %q should not be listed:\n%s", pkg, output)
		}
	}
}

func TestSplitInstalledInventorySeparatesRepoAndForeignPackages(t *testing.T) {
	installed := []string{"base", "git", "neovim", "yay", "custom-tool"}
	foreign := []string{"yay", "custom-tool", "not-installed"}
	repo, external := splitInstalledInventory(installed, foreign)
	wantRepo := []string{"base", "git", "neovim"}
	wantForeign := []string{"custom-tool", "yay"}
	if !reflect.DeepEqual(repo, wantRepo) {
		t.Fatalf("repo inventory = %#v, want %#v", repo, wantRepo)
	}
	if !reflect.DeepEqual(external, wantForeign) {
		t.Fatalf("foreign inventory = %#v, want %#v", external, wantForeign)
	}
}

func TestSplitInstalledInventoryHandlesLargeSystem(t *testing.T) {
	installed := make([]string, 2000)
	for i := range installed {
		installed[i] = fmt.Sprintf("pkg%04d", i)
	}
	repo, foreign := splitInstalledInventory(installed, nil)
	if len(repo) != 2000 || len(foreign) != 0 {
		t.Fatalf("split inventory sizes = repo:%d foreign:%d; want 2000 and 0", len(repo), len(foreign))
	}
	if repo[0] != "pkg0000" || repo[len(repo)-1] != "pkg1999" {
		t.Fatalf("inventory not sorted deterministically: first=%q last=%q", repo[0], repo[len(repo)-1])
	}
}

func TestInventoryPacmanPackagesKeepsDependenciesOutWhenGivenExplicitSet(t *testing.T) {
	// Pacman reports dependency-installed libraries in -Qq, but -Qqen/-Qqem
	// only return explicitly installed targets. The inventory builder must not
	// invent desired packages beyond the explicit list it receives.
	inv := inventoryPacmanPackages([]string{"neovim"}, nil, config.Inventory{})
	if !reflect.DeepEqual(inv.Repo, []string{"neovim"}) {
		t.Fatalf("repo inventory = %#v, want only neovim", inv.Repo)
	}
	for _, dependency := range []string{"libluv", "tree-sitter", "libtermkey"} {
		if config.Contains(inv.Repo, dependency) {
			t.Errorf("dependency %q should not be imported as a desired package", dependency)
		}
	}
}

func TestNormalizeCLIUnitAddsServiceSuffix(t *testing.T) {
	got, err := normalizeCLIUnit("sshd")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sshd.service" {
		t.Fatalf("normalized unit = %q, want sshd.service", got)
	}
	got, err = normalizeCLIUnit("docker.socket")
	if err != nil {
		t.Fatal(err)
	}
	if got != "docker.socket" {
		t.Fatalf("normalized unit = %q, want docker.socket", got)
	}
}

func TestNormalizeCLIUnitRejectsUnsafeUnitNames(t *testing.T) {
	for _, unit := range []string{"../sshd", "hello;touch /tmp/pwn", "", " bad.service"} {
		if _, err := normalizeCLIUnit(unit); err == nil {
			t.Errorf("expected unit %q to be rejected", unit)
		}
	}
}

func TestAddAndRemoveUnitAreIdempotent(t *testing.T) {
	got := addUnit([]string{"sshd.service", "NetworkManager.service"}, "sshd.service")
	if !reflect.DeepEqual(got, []string{"NetworkManager.service", "sshd.service"}) {
		t.Fatalf("add duplicate = %#v", got)
	}
	got = addUnit(got, "cups.service")
	if !reflect.DeepEqual(got, []string{"NetworkManager.service", "cups.service", "sshd.service"}) {
		t.Fatalf("add unit = %#v", got)
	}
	got = removeUnit(got, "cups.service")
	if !reflect.DeepEqual(got, []string{"NetworkManager.service", "sshd.service"}) {
		t.Fatalf("remove unit = %#v", got)
	}
}

func TestInitPlanHidesServiceListsAndPointsToCtl(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	printPlan(planner.Plan{}, servicePlan{changes: []serviceChange{{action: "enable", unit: "sshd.service", current: "disabled"}}}, true, nil, nil, false)
	_ = w.Close()
	os.Stdout = oldStdout
	data, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	output := string(data)
	if strings.Contains(output, "System services to enable") || strings.Contains(output, "System services to disable") || strings.Contains(output, "sshd.service") {
		t.Fatalf("init summary should not list units:\n%s", output)
	}
	if !strings.Contains(output, "zacman ctl") {
		t.Fatalf("init summary should refer to `zacman ctl` for service policy:\n%s", output)
	}
}

func TestCtlIsRecognizedAsZacmanCommand(t *testing.T) {
	if shouldPassthroughToPacman([]string{"ctl", "enable", "sshd"}) {
		t.Fatal("ctl should be handled by Zacman, not passed through to Pacman")
	}
	if shouldPassthroughToPacman([]string{"completion", "zsh"}) {
		t.Fatal("completion should be handled by Cobra, not passed through to Pacman")
	}
}

func TestFirstUnknownCommandSkipsGlobalConfigPath(t *testing.T) {
	got := firstUnknownCommand([]string{"--config", "/tmp/plan", "--no-color", "plan"})
	if got != "plan" {
		t.Fatalf("firstUnknownCommand returned %q, want plan", got)
	}
	got = firstUnknownCommand([]string{"--config=/tmp/other.toml", "typo"})
	if got != "typo" {
		t.Fatalf("firstUnknownCommand returned %q, want typo", got)
	}
}

func TestInventoryPacmanPackagesGroupsKnownStacks(t *testing.T) {
	installed := []string{"git", "linux", "linux-firmware", "pipewire", "wireplumber", "niri", "sddm", "ttf-jetbrains-mono-nerd", "amd-ucode"}
	inv := inventoryPacmanPackages(installed, nil, config.Inventory{})
	for _, pkg := range []string{"git"} {
		if !config.Contains(inv.Repo, pkg) {
			t.Errorf("ordinary package %q should remain in repo inventory: %#v", pkg, inv.Repo)
		}
	}
	for _, pkg := range []string{"linux", "linux-firmware", "amd-ucode"} {
		if !config.Contains(inv.KernelPkgs, pkg) {
			t.Errorf("kernel package %q not classified: %#v", pkg, inv.KernelPkgs)
		}
	}
	for _, pkg := range []string{"pipewire", "wireplumber"} {
		if !config.Contains(inv.AudioPkgs, pkg) {
			t.Errorf("audio package %q not classified: %#v", pkg, inv.AudioPkgs)
		}
	}
	for _, pkg := range []string{"niri"} {
		if !config.Contains(inv.DesktopPkgs, pkg) {
			t.Errorf("desktop package %q not classified: %#v", pkg, inv.DesktopPkgs)
		}
	}
	for _, pkg := range []string{"sddm"} {
		if !config.Contains(inv.GreeterPkgs, pkg) {
			t.Errorf("greeter package %q not classified: %#v", pkg, inv.GreeterPkgs)
		}
	}
	if !config.Contains(inv.FontPkgs, "ttf-jetbrains-mono-nerd") {
		t.Errorf("font package not classified: %#v", inv.FontPkgs)
	}
}

func TestCleanLocaleValueDropsLocalectlSentinels(t *testing.T) {
	for _, value := range []string{"n/a", "N/A", " unset ", "(unset)", "none", ""} {
		if got := cleanLocaleValue(value); got != "" {
			t.Errorf("cleanLocaleValue(%q) = %q, want empty", value, got)
		}
	}
	if got := cleanLocaleValue(" us,ru "); got != "us,ru" {
		t.Fatalf("cleanLocaleValue did not preserve keyboard layout list: %q", got)
	}
}

func TestEnsureDestinationDirectoryCreatesDirectoriesAndRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "one", "two", "three")
	if err := ensureDestinationDirectory(missing, nil); err != nil {
		t.Fatalf("ensure destination: %v", err)
	}
	info, err := os.Stat(missing)
	if err != nil || !info.IsDir() {
		t.Fatalf("directory not created: info=%v err=%v", info, err)
	}
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "symlink")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := ensureDestinationDirectory(filepath.Join(link, "child"), nil); err == nil {
		t.Fatal("expected symlink destination to be rejected")
	}
}

func TestCobraInitHelpAndYesAliasesAreSingleFlags(t *testing.T) {
	root := New("test").newCobraRoot()
	cmd, _, err := root.Find([]string{"init"})
	if err != nil {
		t.Fatalf("find init command: %v", err)
	}
	cmd.InitDefaultHelpFlag()
	help := cmd.Flags().Lookup("help")
	if help == nil || help.Shorthand != "h" || cmd.Flags().ShorthandLookup("h") != help {
		t.Fatal("-h and --help should be aliases of the same Cobra flag")
	}
	yes := cmd.Flags().Lookup("yes")
	if yes == nil || yes.Shorthand != "y" || cmd.Flags().ShorthandLookup("y") != yes {
		t.Fatal("-y and --yes should be aliases of the same Cobra flag")
	}
	usage := cmd.Flags().FlagUsages()
	if strings.Count(usage, "--help") != 1 || strings.Count(usage, "--yes") != 1 {
		t.Fatalf("help and yes aliases should each render one row, got:\n%s", usage)
	}
}

func TestLegacyRunAndNpxCommandsAreRemoved(t *testing.T) {
	for _, args := range [][]string{{"run", "npx", "eslint"}, {"npx", "eslint"}} {
		err := New("test").Run(args)
		if err == nil || !strings.Contains(err.Error(), "unknown Zacman command") {
			t.Fatalf("expected removed command %#v to fail as unknown; got %v", args, err)
		}
	}
}

func TestAppendDetectedInventoryPreservesExistingConfigValues(t *testing.T) {
	cfg := config.Default()
	cfg.Packages.Repo = []string{"git", "neovim"}
	cfg.Fonts.Pkgs = []string{"ttf-font-a"}
	cfg.Locale.SystemLanguage = "fr_FR.UTF-8"
	got := appendDetectedInventory(cfg, config.Inventory{
		Repo: []string{"git", "ripgrep"}, FontPkgs: []string{"ttf-font-a", "ttf-font-b"},
		SystemLanguage: "en_US.UTF-8", KeyboardLayout: "us", Flatpak: []string{"org.gimp.GIMP"},
	})
	if !reflect.DeepEqual(got.Packages.Repo, []string{"git", "neovim", "ripgrep"}) {
		t.Fatalf("repo entries were not appended without replacing prior entries: %#v", got.Packages.Repo)
	}
	if !reflect.DeepEqual(got.Fonts.Pkgs, []string{"ttf-font-a", "ttf-font-b"}) {
		t.Fatalf("font entries were not appended idempotently: %#v", got.Fonts.Pkgs)
	}
	if got.Locale.SystemLanguage != "fr_FR.UTF-8" || got.Locale.KeyboardLayout != "us" {
		t.Fatalf("existing locale was overwritten or empty locale was not filled: %#v", got.Locale)
	}
	if !reflect.DeepEqual(got.Flatpak.Pkgs, []string{"org.gimp.GIMP"}) {
		t.Fatalf("detected Flatpak package missing: %#v", got.Flatpak.Pkgs)
	}
}

func TestParseAURBatchRequiresConsistentPrefix(t *testing.T) {
	got, ok, err := parseAURBatch([]string{"aur:yay", "aur:paru", "aur:yay"})
	if err != nil || !ok || !reflect.DeepEqual(got, []string{"paru", "yay"}) {
		t.Fatalf("parseAURBatch returned %#v, %t, %v", got, ok, err)
	}
	if _, ok, err := parseAURBatch([]string{"aur:yay", "neovim"}); err == nil || !ok {
		t.Fatalf("mixed AUR/Pacman list should fail: ok=%t err=%v", ok, err)
	}
	if _, ok, err := parseAURBatch([]string{"aur:../bad"}); err == nil || !ok {
		t.Fatalf("invalid AUR target should fail: ok=%t err=%v", ok, err)
	}
}

func TestParseBlackArchBatchRequiresConsistentPrefix(t *testing.T) {
	got, ok, err := parseBlackArchBatch([]string{"blackarch:commix", "blackarch:recon-ng"})
	if err != nil || !ok || !reflect.DeepEqual(got, []string{"commix", "recon-ng"}) {
		t.Fatalf("parseBlackArchBatch returned %#v, %t, %v", got, ok, err)
	}
	if _, ok, err := parseBlackArchBatch([]string{"blackarch:commix", "neovim"}); err == nil || !ok {
		t.Fatalf("mixed BlackArch/Pacman list should fail: ok=%t err=%v", ok, err)
	}
}

func TestFilterPlanSatisfiedByInstalledProviders(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fake-pacman")
	script := "#!/bin/sh\n[ \"$1\" = \"-T\" ] || exit 9\nshift\n" +
		"for target in \"$@\"; do [ \"$target\" = \"fresh-pkg\" ] && echo \"$target\"; done\nexit 127\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	plan := planner.Plan{
		Install:        []string{"exiftool", "xxd", "fresh-pkg"},
		DesiredMissing: []string{"exiftool", "xxd", "fresh-pkg"},
	}
	got, err := filterInstalledProviderTargets(pacman.Client{Binary: bin}, plan)
	if err != nil {
		t.Fatalf("filterInstalledProviderTargets() error = %v", err)
	}
	if !reflect.DeepEqual(got.Install, []string{"fresh-pkg"}) {
		t.Fatalf("Install = %#v, want only fresh-pkg", got.Install)
	}
	if !reflect.DeepEqual(got.DesiredMissing, []string{"fresh-pkg"}) {
		t.Fatalf("DesiredMissing = %#v, want only fresh-pkg", got.DesiredMissing)
	}
}
