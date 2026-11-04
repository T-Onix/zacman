package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/T-Onix/zacman/internal/config"
	"github.com/T-Onix/zacman/internal/pacman"
	"github.com/T-Onix/zacman/internal/pkgmanager"
	"github.com/T-Onix/zacman/internal/planner"
	"github.com/T-Onix/zacman/internal/services"
	"github.com/T-Onix/zacman/internal/state"
	"github.com/T-Onix/zacman/internal/transaction"
)

type App struct{ version string }

func New(version string) *App { return &App{version: version} }

type serviceChange struct {
	action, unit string
	current      services.Status
}
type servicePlan struct {
	changes  []serviceChange
	already  []string
	issues   []string
	warnings []string
}

type options struct {
	configPath string
	args       []string
	noColor    bool
}

func (a *App) Run(args []string) error {
	// Keep Pacman's full CLI available and preserve its exact argument vector,
	// terminal streams, prompts, and exit status. Zacman-owned commands are
	// handled by Cobra below.
	if shouldPassthroughToPacman(args) {
		if len(args) > 0 && args[0] == "pacman" {
			args = args[1:]
		}
		filtered := make([]string, 0, len(args))
		for _, arg := range args {
			if arg != "--no-color" {
				filtered = append(filtered, arg)
			}
		}
		return pacman.New().Passthrough(filtered...)
	}

	args = normalizeLegacyInitFlags(args)
	previousNoColor := noColorOverride
	defer func() { noColorOverride = previousNoColor }()
	root := a.newCobraRoot()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		// Preserve the old concise error wording for users familiar with earlier
		// Zacman releases while Cobra supplies the command-tree implementation.
		if strings.Contains(err.Error(), "unknown command") {
			if first := firstUnknownCommand(args); first != "" {
				return fmt.Errorf("unknown Zacman command %q; use 'zacman --help' or a Pacman option such as 'zacman -Ss <term>'", first)
			}
		}
		return err
	}
	return nil
}

func firstUnknownCommand(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--config":
			// The following token is a path, not a command candidate.
			if i+1 < len(args) {
				i++
			}
			continue
		case strings.HasPrefix(arg, "--config=") || arg == "--no-color":
			continue
		case strings.HasPrefix(arg, "-"):
			continue
		case !isZacmanCommand(arg):
			return arg
		}
	}
	return ""
}

func normalizeLegacyInitFlags(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i, arg := range out {
		switch arg {
		case "-dfc":
			out[i] = "--default-file-config"
		case "-axp":
			out[i] = "--add-existing-packages"
		case "-du":
			out[i] = "--dont-update"
		case "-dc":
			out[i] = "--dedupe-config"
		}
	}
	return out
}

func shouldPassthroughToPacman(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "pacman" {
		return true
	}
	// Keep Zacman's conventional long help/version options; Pacman's native
	// help/version remain available with -h/-V or `zacman pacman --help`.
	if args[0] == "help" || args[0] == "--help" || args[0] == "--version" {
		return false
	}

	// Options before a Zacman subcommand are Zacman globals. If no Zacman
	// command follows and a Pacman switch does, forward the original argv so
	// Pacman's own --config, --dbpath, --root, and other flags remain intact.
	for i := 0; i < len(args); {
		arg := args[i]
		switch {
		case arg == "--config":
			if i+1 < len(args) {
				i += 2
				continue
			}
			return false
		case strings.HasPrefix(arg, "--config=") || arg == "--no-color":
			i++
			continue
		case isZacmanCommand(arg):
			return false
		case strings.HasPrefix(arg, "-"):
			return true
		default:
			return false
		}
	}
	return false
}

func isZacmanCommand(arg string) bool {
	switch arg {
	case "init", "config", "diff", "status", "install", "uninstall", "update", "doctor", "ctl", "repo", "completion", "help", "--help", "--version":
		return true
	default:
		return false
	}
}

func parseOptions(args []string) (options, error) {
	opts := options{configPath: config.DefaultPath}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--config":
			if i+1 >= len(args) {
				return opts, errors.New("--config requires a path")
			}
			i++
			if args[i] == "" {
				return opts, errors.New("--config path cannot be empty")
			}
			opts.configPath = args[i]
		case strings.HasPrefix(arg, "--config="):
			opts.configPath = strings.TrimPrefix(arg, "--config=")
			if opts.configPath == "" {
				return opts, errors.New("--config path cannot be empty")
			}
		case arg == "--no-color":
			opts.noColor = true
		default:
			opts.args = append(opts.args, arg)
		}
	}
	return opts, nil
}

func (a *App) initCommand(path string, args []string) error {
	for _, arg := range args {
		switch arg {
		case "-dfc", "--default-file-config":
			if len(args) != 1 {
				return errors.New("init -dfc cannot be combined with other init options")
			}
			return a.createDefaultConfig(path)
		case "-axp", "--add-existing-packages":
			if len(args) != 1 {
				return errors.New("init -axp cannot be combined with reconciliation options")
			}
			return a.addExistingPackages(path)
		}
	}

	autoDedupe, skipPrompt := false, false
	for _, arg := range args {
		switch arg {
		case "--dedupe-config":
			autoDedupe = true
		case "--yes", "-y":
			skipPrompt = true
		}
	}
	duplicates, err := config.DuplicatePackageEntries(path)
	if err != nil {
		return err
	}
	if len(duplicates) > 0 {
		for _, arg := range args {
			if arg == "--dry-run" {
				fmt.Fprintln(os.Stdout, paint("yellow", "Dry run: duplicate entries were detected but the config was not modified."))
				for _, duplicate := range duplicates {
					fmt.Fprintf(os.Stdout, "  • %s\n", duplicate)
				}
				return errors.New("duplicate package entries prevent a valid dry-run plan; rerun 'zacman init' to confirm cleanup or 'zacman init -dc' to clean automatically")
			}
		}
		fmt.Fprintln(os.Stdout, paint("yellow", "Duplicate package entries detected:"))
		for _, duplicate := range duplicates {
			fmt.Fprintf(os.Stdout, "  • %s\n", duplicate)
		}
		approved := autoDedupe || skipPrompt
		if !approved {
			approved, err = confirmDefaultNo(paint("lavender", "Remove repeated package entries from zacman.toml? [y/N] "))
			if err != nil {
				return err
			}
		}
		if !approved {
			return errors.New("duplicate package entries were left unchanged; rerun 'zacman init -dc' to remove duplicates without a prompt")
		}
		removed, err := config.RepairDuplicatePackageEntries(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "%s Removed %d duplicate package entr(y/ies) from %s; first occurrences were kept.\n", paint("mint", "✓"), len(removed), path)
	}
	return a.reconcile(path, args)
}

// addExistingPackages imports only packages Pacman marks as explicitly
// installed and appends them to the primary manifest. Existing package entries,
// settings, references, and user comments are retained; no inventory files are
// created or refreshed and no package transaction is run.
func (a *App) addExistingPackages(path string) error {
	if err := requireRoot("import the installed package inventory and update Zacman's state"); err != nil {
		return err
	}
	unlock, err := transaction.AcquireLock("")
	if err != nil {
		return err
	}
	defer unlock()

	cfg, err := config.LoadBase(path)
	if err != nil {
		return err
	}
	if _, _, err := state.Load(""); err != nil {
		return fmt.Errorf("cannot import packages while managed state is unreadable: %w", err)
	}
	client := pacman.New()
	// -Qqen and -Qqem return explicitly installed packages only. Dependencies
	// remain installed and Pacman will resolve them normally for future installs.
	repoInventory, err := client.ExplicitNativePackages()
	if err != nil {
		return err
	}
	foreignInventory, err := client.ExplicitForeignPackages()
	if err != nil {
		return err
	}
	repoInventory = config.SortedUnique(repoInventory)
	foreignInventory = config.SortedUnique(foreignInventory)

	// Build a snapshot for the state baseline, then append the snapshot into the
	// existing manifest. We deliberately do not use any generated reference file.
	detected := inventoryPacmanPackages(repoInventory, foreignInventory, config.Inventory{})
	detected, managerWarnings, err := collectManagerInventory(detected)
	if err != nil {
		return err
	}
	cfg = appendDetectedInventory(cfg, detected)
	if err := config.Validate(cfg); err != nil {
		return err
	}
	if err := config.WriteAtomic(path, cfg); err != nil {
		return fmt.Errorf("append installed package inventory to %s: %w", path, err)
	}

	// The baseline represents only package targets that are installed now; do
	// not adopt unrelated desired entries from the manifest as already installed.
	stateConfig := config.Default()
	stateConfig.Packages.Repo = detected.Repo
	stateConfig.Kernel.Pkgs = detected.KernelPkgs
	stateConfig.Audio.Pkgs = detected.AudioPkgs
	stateConfig.Desktop.Pkgs = detected.DesktopPkgs
	stateConfig.Fonts.Pkgs = detected.FontPkgs
	stateConfig.Greeter.Pkgs = detected.GreeterPkgs
	stateConfig.Mouse.Pkgs = detected.MousePkgs
	stateConfig.Mouse.Theme, stateConfig.Mouse.ThemeSource, stateConfig.Mouse.ThemePackage, stateConfig.Mouse.Size = detected.MouseTheme, detected.MouseThemeSource, detected.MouseThemePackage, detected.MouseSize
	if err := state.Save("", state.State{
		Version: state.Version, ManagedPackages: config.AllPacmanPackages(stateConfig), LastSuccessful: time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("manifest inventory was appended, but the managed baseline could not be updated; rerun 'sudo zacman init -axp': %w", err)
	}

	explicitCount := len(repoInventory) + len(foreignInventory)
	fmt.Fprintf(os.Stdout, "%s Appended %d explicitly installed Pacman packages to %s\n", paint("mint", "✓"), explicitCount, path)
	fmt.Fprintf(os.Stdout, "  Repository: %d | Kernel: %d | Audio: %d | Desktop: %d | Fonts: %d | Greeter: %d | Foreign: %d\n", len(detected.Repo), len(detected.KernelPkgs), len(detected.AudioPkgs), len(detected.DesktopPkgs), len(detected.FontPkgs), len(detected.GreeterPkgs), len(detected.Foreign))
	fmt.Fprintf(os.Stdout, "  Flatpak: %d system / %d user | uv: %d | pip3: %d | npm: %d | Cargo: %d | Go tools: %d\n", len(detected.Flatpak), len(detected.FlatpakUser), len(detected.PythonUV), len(detected.PythonPip3), len(detected.NodeNPM), len(detected.RustCargo), len(detected.Golang))
	for _, warning := range managerWarnings {
		fmt.Fprintf(os.Stderr, "%s %s\n", paint("yellow", "Warning:"), warning)
	}
	fmt.Fprintln(os.Stdout, "  Existing TOML values and comments are preserved; only missing package entries and empty detected settings were added.")
	fmt.Fprintln(os.Stdout, "  Dependency-installed packages were not imported; Pacman still resolves dependencies normally.")
	fmt.Fprintln(os.Stdout, "  No package transactions or service changes were performed, and no extra config files were created.")
	return nil
}

// appendDetectedInventory adds discovered values without replacing user values.
// Values remain in their existing order; newly imported values are appended in
// deterministic query order. A package already declared in packages.repo is
// left there for `zacman doctor` to organize rather than duplicated into a
// dedicated package category automatically.
func appendDetectedInventory(cfg config.Config, inv config.Inventory) config.Config {
	cfg.Packages.Repo = appendUniquePreservingOrder(cfg.Packages.Repo, inv.Repo)
	cfg.Packages.Foreign.Items = appendUniquePreservingOrder(cfg.Packages.Foreign.Items, inv.Foreign)
	repoDeclared := makeSet(cfg.Packages.Repo)
	appendCategory := func(current, incoming []string) []string {
		filtered := make([]string, 0, len(incoming))
		for _, item := range incoming {
			if !repoDeclared[item] {
				filtered = append(filtered, item)
			}
		}
		return appendUniquePreservingOrder(current, filtered)
	}
	cfg.Kernel.Pkgs = appendCategory(cfg.Kernel.Pkgs, inv.KernelPkgs)
	cfg.Audio.Pkgs = appendCategory(cfg.Audio.Pkgs, inv.AudioPkgs)
	cfg.Desktop.Pkgs = appendCategory(cfg.Desktop.Pkgs, inv.DesktopPkgs)
	cfg.Fonts.Pkgs = appendCategory(cfg.Fonts.Pkgs, inv.FontPkgs)
	cfg.Greeter.Pkgs = appendCategory(cfg.Greeter.Pkgs, inv.GreeterPkgs)
	cfg.Mouse.Pkgs = appendCategory(cfg.Mouse.Pkgs, inv.MousePkgs)

	cfg.Flatpak.Pkgs = appendUniquePreservingOrder(cfg.Flatpak.Pkgs, inv.Flatpak)
	cfg.Flatpak.UserPkgs = appendUniquePreservingOrder(cfg.Flatpak.UserPkgs, inv.FlatpakUser)
	cfg.Desktop.Config = appendUniquePreservingOrder(cfg.Desktop.Config, inv.DesktopConfig)
	cfg.Audio.Config = appendUniquePreservingOrder(cfg.Audio.Config, inv.AudioConfig)
	cfg.Kernel.Config = appendUniquePreservingOrder(cfg.Kernel.Config, inv.KernelConfig)
	cfg.Fonts.Config = appendUniquePreservingOrder(cfg.Fonts.Config, inv.FontConfig)
	cfg.Python.UV.Pkgs = appendUniquePreservingOrder(cfg.Python.UV.Pkgs, inv.PythonUV)
	cfg.Python.UV.Config = appendUniquePreservingOrder(cfg.Python.UV.Config, inv.PythonUVConfig)
	cfg.Python.Pip3.Pkgs = appendUniquePreservingOrder(cfg.Python.Pip3.Pkgs, inv.PythonPip3)
	cfg.Python.Pip3.Config = appendUniquePreservingOrder(cfg.Python.Pip3.Config, inv.PythonPip3Config)
	cfg.Node.NPM.Pkgs = appendUniquePreservingOrder(cfg.Node.NPM.Pkgs, inv.NodeNPM)
	cfg.Node.NPM.Config = appendUniquePreservingOrder(cfg.Node.NPM.Config, inv.NodeNPMConfig)
	cfg.Rust.Cargo.Pkgs = appendUniquePreservingOrder(cfg.Rust.Cargo.Pkgs, inv.RustCargo)
	cfg.Rust.Cargo.Config = appendUniquePreservingOrder(cfg.Rust.Cargo.Config, inv.RustCargoConfig)
	cfg.Golang.Pkgs = appendUniquePreservingOrder(cfg.Golang.Pkgs, inv.Golang)
	cfg.Golang.Config = appendUniquePreservingOrder(cfg.Golang.Config, inv.GolangConfig)

	if cfg.Greeter.System == "" {
		cfg.Greeter.System = inv.GreeterSystem
	}
	if cfg.Mouse.Theme == "" {
		cfg.Mouse.Theme = inv.MouseTheme
	}
	if cfg.Mouse.ThemeSource == "" {
		cfg.Mouse.ThemeSource = inv.MouseThemeSource
	}
	if cfg.Mouse.ThemePackage == "" {
		cfg.Mouse.ThemePackage = inv.MouseThemePackage
	}
	if cfg.Mouse.Size == 0 {
		cfg.Mouse.Size = inv.MouseSize
	}
	if cfg.Greeter.System == "sddm" {
		if cfg.Greeter.Theme == "" {
			cfg.Greeter.Theme = inv.GreeterTheme
		}
		if cfg.Greeter.ThemeSource == "" {
			cfg.Greeter.ThemeSource = inv.GreeterThemeSource
		}
		if cfg.Greeter.ThemePackage == "" {
			cfg.Greeter.ThemePackage = inv.GreeterThemePackage
		}
	}
	if cfg.Locale.SystemLanguage == "" {
		cfg.Locale.SystemLanguage = inv.SystemLanguage
	}
	if cfg.Locale.KeyboardLayout == "" {
		cfg.Locale.KeyboardLayout = inv.KeyboardLayout
	}
	if cfg.Locale.KeyboardVariant == "" {
		cfg.Locale.KeyboardVariant = inv.KeyboardVariant
	}
	if cfg.Locale.KeyboardModel == "" {
		cfg.Locale.KeyboardModel = inv.KeyboardModel
	}
	if cfg.Locale.KeyboardOptions == "" {
		cfg.Locale.KeyboardOptions = inv.KeyboardOptions
	}
	return cfg
}

func appendUniquePreservingOrder(current, additions []string) []string {
	result := append([]string(nil), current...)
	seen := makeSet(result)
	for _, item := range additions {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	return result
}

// splitInstalledInventory separates Pacman's full installed-package inventory
// into packages backed by sync repositories and packages reported as foreign.
func splitInstalledInventory(installed, foreign []string) ([]string, []string) {
	foreignSet := makeSet(foreign)
	repoInventory := make([]string, 0, len(installed))
	foreignInventory := make([]string, 0, len(foreign))
	for _, pkg := range installed {
		if foreignSet[pkg] {
			foreignInventory = append(foreignInventory, pkg)
		} else {
			repoInventory = append(repoInventory, pkg)
		}
	}
	return config.SortedUnique(repoInventory), config.SortedUnique(foreignInventory)
}

func (a *App) createDefaultConfig(path string) error {
	if err := requireRoot("create the default configuration file"); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("config already exists at %s; refusing to overwrite it", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect config path: %w", err)
	}
	if err := os.MkdirAll(config.ConfigDirectory(path), 0755); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	cfg := config.Default()
	if err := config.WriteAtomic(path, cfg); err != nil {
		return err
	}
	// The main file was just created, so run ownership repair once more to make
	// it editable by the invoking user when called through sudo.
	if os.Geteuid() == 0 && filepath.Clean(path) == filepath.Clean(config.DefaultPath) && strings.TrimSpace(os.Getenv("SUDO_UID")) != "" {
		if err := config.FixOwnership(path); err != nil {
			return fmt.Errorf("default configuration was created, but config ownership could not be assigned to the invoking user: %w", err)
		}
	}
	fmt.Fprintf(os.Stdout, "%s Created empty default configuration: %s\n", paint("mint", "✓"), path)
	fmt.Fprintln(os.Stdout, "No packages or services were installed, removed, enabled, or disabled; no extra config files were created.")
	return nil
}

func (a *App) configCommand(path string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: zacman config validate|show")
	}
	switch args[0] {
	case "validate":
		cfg, err := config.Load(path)
		if err != nil {
			return err
		}
		if err := config.Validate(cfg); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Configuration valid: %s\n", path)
		return nil
	case "show":
		data, err := config.ReadRaw(path)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(data)
		return err
	default:
		return fmt.Errorf("unknown config command %q; expected validate or show", args[0])
	}
}

func (a *App) showDiffOrStatus(path, mode string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	client := pacman.New()
	installed, err := client.InstalledPackages()
	if err != nil {
		return err
	}
	foreign, err := client.ForeignPackages()
	if err != nil {
		return err
	}
	baseline, hasBaseline, err := state.Load("")
	if err != nil {
		return err
	}
	desiredPackages := config.AllPacmanPackages(cfg)
	p := planner.BuildWithRetained(desiredPackages, baseline.ManagedPackages, installed, cfg.Packages.Foreign.Items, hasBaseline)
	p, err = filterInstalledProviderTargets(client, p)
	if err != nil {
		return fmt.Errorf("check installed package providers: %w", err)
	}
	sp := planServices(cfg, p.Install)
	packageIssues := unsupportedInstalledPackages(cfg, foreign)
	settingsChanges, err := pacman.SystemSettingsDiff(cfg.Pacman.ILoveCandy, cfg.Mirrors.Servers, cfg.Mirrors.BlackarchServers)
	if err != nil {
		return err
	}
	external := planExternalPackages(cfg, p.Install)
	systemPlan := buildSystemConfigPlan(cfg, path, len(p.Install) > 0)
	extraChanges := external.hasChanges() || len(external.Issues) > 0 || systemPlan.hasChanges()
	printPlan(p, sp, hasBaseline, packageIssues, settingsChanges, true, extraChanges)
	printExternalPlan(external)
	printSystemConfigPlan(systemPlan)
	printCategoryWarnings(cfg)
	for _, issue := range external.Issues {
		fmt.Fprintf(os.Stdout, "%s %s\n", paint("yellow", "Warning:"), issue)
	}
	if mode == "status" {
		fmt.Fprintf(os.Stdout, "\nConfig: %s\nState baseline: %s\n", path, yesNo(hasBaseline))
		fmt.Fprintf(os.Stdout, "Managed Pacman packages: %d | Foreign inventory: %d | Installed packages: %d\n", len(desiredPackages), len(cfg.Packages.Foreign.Items), len(installed))
	}
	return nil
}

func printPlan(p planner.Plan, sp servicePlan, hasBaseline bool, packageIssues []string, settingsChanges []string, options ...bool) {
	showServiceChanges := len(options) > 0 && options[0]
	fmt.Fprintln(os.Stdout, paint("lavender", "Zacman")+" "+paint("subtle", "—")+" "+paint("cyan", "Configuration diff"))
	fmt.Fprintln(os.Stdout)
	printPackageList("Packages to install", p.Install, "+")
	printPackageList("Packages to remove (previously managed only)", p.Remove, "-")
	if len(settingsChanges) > 0 {
		fmt.Fprintln(os.Stdout, paint("cyan", "Pacman settings:"))
		for _, change := range settingsChanges {
			fmt.Fprintf(os.Stdout, "  %s %s\n", paint("mint", "+"), change)
		}
	}
	if !hasBaseline {
		fmt.Fprintln(os.Stdout, "Note: no prior managed-state baseline exists; undeclared packages will not be removed.")
	}
	if showServiceChanges {
		fmt.Fprintln(os.Stdout)
		printServiceList("System services to enable", sp.changes, "enable")
		printServiceList("System services to disable", sp.changes, "disable")
		if len(sp.already) > 0 {
			fmt.Fprintln(os.Stdout, "Services already in requested state:")
			for _, item := range sp.already {
				fmt.Fprintf(os.Stdout, "  = %s\n", item)
			}
		}
		if len(sp.issues) > 0 {
			fmt.Fprintln(os.Stdout, "Service issues:")
			for _, issue := range sp.issues {
				fmt.Fprintf(os.Stdout, "  ! %s\n", issue)
			}
		}
	} else if len(sp.changes) > 0 || len(sp.issues) > 0 || len(sp.warnings) > 0 {
		fmt.Fprintln(os.Stdout, paint("subtle", "Service policy is managed separately; inspect 'zacman ctl'."))
	}
	if len(packageIssues) > 0 {
		fmt.Fprintln(os.Stdout, "Unsupported package declarations:")
		for _, issue := range packageIssues {
			fmt.Fprintf(os.Stdout, "  ! %s\n", issue)
		}
	}
	if len(sp.warnings) > 0 {
		fmt.Fprintln(os.Stdout, "Warnings:")
		for _, warning := range sp.warnings {
			fmt.Fprintf(os.Stdout, "  ! %s\n", warning)
		}
	}
	extra := len(options) > 1 && options[1]
	if len(p.Install)+len(p.Remove)+len(sp.changes)+len(settingsChanges) == 0 && len(sp.issues) == 0 && len(packageIssues) == 0 && !extra {
		fmt.Fprintln(os.Stdout, paint("mint", "✓ No changes required."))
	} else {
		fmt.Fprintln(os.Stdout, paint("subtle", "No changes have been made yet."))
	}
}

func printPackageList(title string, values []string, marker string) {
	fmt.Fprintln(os.Stdout, paint("cyan", title)+":")
	if len(values) == 0 {
		fmt.Fprintln(os.Stdout, "  (none)")
		return
	}
	for _, item := range values {
		color := "mint"
		if marker == "-" {
			color = "red"
		}
		fmt.Fprintf(os.Stdout, "  %s %s\n", paint(color, marker), item)
	}
}

func printServiceList(title string, values []serviceChange, action string) {
	fmt.Fprintln(os.Stdout, paint("cyan", title)+":")
	count := 0
	for _, c := range values {
		if c.action == action {
			fmt.Fprintf(os.Stdout, "  %s %s (currently %s)\n", paint("mint", "+"), c.unit, c.current)
			count++
		}
	}
	if count == 0 {
		fmt.Fprintln(os.Stdout, "  (none)")
	}
}

func servicesWithGreeter(cfg config.Config) config.ServiceConfig {
	policy := cfg.Services
	if cfg.Greeter.System != "" {
		policy.Enable = config.SortedUnique(append(policy.Enable, cfg.Greeter.System+".service"))
	}
	return policy
}

func planServices(cfg config.Config, plannedPackages ...[]string) servicePlan {
	m := services.New()
	p := servicePlan{}
	policy := servicesWithGreeter(cfg)
	pending := map[string]bool{}
	if len(plannedPackages) > 0 {
		for _, pkg := range plannedPackages[0] {
			pending[pkg] = true
		}
	}
	greeterPackage := map[string]string{"sddm.service": "sddm", "gdm.service": "gdm", "lightdm.service": "lightdm", "greetd.service": "greetd", "ly.service": "ly", "emptty.service": "emptty"}
	for _, unit := range policy.Enable {
		status, err := m.IsEnabled(unit)
		if err != nil {
			p.issues = append(p.issues, fmt.Sprintf("%s: %v", unit, err))
			continue
		}
		if status == services.Enabled {
			p.already = append(p.already, unit+" (enabled)")
			continue
		}
		if status == services.Missing && pending[greeterPackage[unit]] && greeterPackage[unit] != "" {
			p.changes = append(p.changes, serviceChange{action: "enable", unit: unit, current: status})
			continue
		}
		if status == services.Missing || status == services.Masked || status == services.Static {
			p.issues = append(p.issues, fmt.Sprintf("cannot enable %s: unit state is %s", unit, status))
			continue
		}
		p.changes = append(p.changes, serviceChange{action: "enable", unit: unit, current: status})
	}
	for _, unit := range policy.Disable {
		if isCriticalUnit(unit) {
			p.warnings = append(p.warnings, fmt.Sprintf("disabling %s may disrupt networking, remote access, login, or core system operation", unit))
		}
		status, err := m.IsEnabled(unit)
		if err != nil {
			p.issues = append(p.issues, fmt.Sprintf("%s: %v", unit, err))
			continue
		}
		if status == services.Disabled {
			p.already = append(p.already, unit+" (disabled)")
			continue
		}
		if status == services.Missing || status == services.Masked || status == services.Static {
			p.issues = append(p.issues, fmt.Sprintf("cannot disable %s: unit state is %s", unit, status))
			continue
		}
		p.changes = append(p.changes, serviceChange{action: "disable", unit: unit, current: status})
	}
	sort.Strings(p.already)
	sort.Strings(p.issues)
	sort.Strings(p.warnings)
	sort.Slice(p.changes, func(i, j int) bool {
		if p.changes[i].action == p.changes[j].action {
			return p.changes[i].unit < p.changes[j].unit
		}
		return p.changes[i].action < p.changes[j].action
	})
	return p
}

func (a *App) install(path string, packages []string, dontUpdate bool) error {
	if len(packages) == 0 {
		return errors.New("usage: zacman install <package...>")
	}
	aurPackages, isAUR, aurErr := parseAURBatch(packages)
	if aurErr != nil {
		return aurErr
	}
	if isAUR {
		// Normalize known historical selectors before installing and recording
		// the package. AUR targets are PKGBUILD package names, not necessarily
		// the installed binary's command name.
		aurPackages = canonicalAURPackages(aurPackages)
		if err := requireRoot("install AUR packages and update the Zacman configuration"); err != nil {
			return err
		}
		unlock, err := transaction.AcquireLock("")
		if err != nil {
			return err
		}
		defer unlock()
		cfg, err := config.LoadBase(path)
		if err != nil {
			return err
		}
		if !cfg.Packages.AUR.Enabled {
			return errors.New("AUR installation is disabled; set [packages.aur] enabled = true before using 'zacman install aur:<package>'")
		}
		if err := installAURPackages(aurPackages, dontUpdate); err != nil {
			return err
		}
		cfg.Packages.AUR.Items = canonicalAURPackages(append(cfg.Packages.AUR.Items, aurPackages...))
		if err := config.WriteAtomic(path, cfg); err != nil {
			return fmt.Errorf("AUR packages may be installed, but the main manifest could not be updated: %w", err)
		}
		fmt.Fprintf(os.Stdout, "%s Recorded %d AUR package(s) in the manifest.\n", paint("mint", "✓"), len(aurPackages))
		return nil
	}
	blackarchPackages, isBlackArch, blackarchErr := parseBlackArchBatch(packages)
	if blackarchErr != nil {
		return blackarchErr
	}
	if isBlackArch {
		if err := requireRoot("install BlackArch repository packages"); err != nil {
			return err
		}
		unlock, err := transaction.AcquireLock("")
		if err != nil {
			return err
		}
		enableErr := a.enableBlackArchRepository()
		unlock()
		if enableErr != nil {
			return enableErr
		}
		return a.install(path, blackarchPackages, dontUpdate)
	}
	kind, managerPackages, external, parseErr := parseManagerBatch(packages)
	if parseErr != nil {
		return parseErr
	}
	if external {
		return a.installExternal(path, kind, managerPackages, dontUpdate)
	}
	packages = config.SortedUnique(packages)
	warningConfig := config.Default()
	warningConfig.Packages.Repo = append([]string(nil), packages...)
	printCategoryWarnings(warningConfig)
	if err := config.ValidatePackages(packages); err != nil {
		return err
	}
	if err := requireRoot("install packages and update system configuration"); err != nil {
		return err
	}
	unlock, err := transaction.AcquireLock("")
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := config.LoadBase(path)
	if err != nil {
		return err
	}
	effectiveCfg, err := config.Load(path)
	if err != nil {
		return err
	}
	baseline, hasBaseline, err := state.Load("")
	if err != nil {
		return fmt.Errorf("cannot install while managed state is unreadable: %w", err)
	}
	client := pacman.New()
	installed, err := client.InstalledPackages()
	if err != nil {
		return err
	}
	foreign, err := client.ForeignPackages()
	if err != nil {
		return err
	}
	foreignSet := makeSet(foreign)
	installedSet := makeSet(installed)
	for _, pkg := range packages {
		if installedSet[pkg] && foreignSet[pkg] {
			return fmt.Errorf("%s is installed as a foreign package; Zacman only manages repository packages and will not reclassify it as AUR", pkg)
		}
	}
	missing := make([]string, 0, len(packages))
	for _, pkg := range packages {
		if !installedSet[pkg] {
			missing = append(missing, pkg)
		}
	}
	providedAlready, err := client.SatisfiedByInstalledProviders(missing)
	if err != nil {
		return fmt.Errorf("check whether requested packages are already provided by installed packages: %w", err)
	}
	providedAlreadySet := makeSet(providedAlready)
	remainingMissing := make([]string, 0, len(missing))
	for _, pkg := range missing {
		if !providedAlreadySet[pkg] {
			remainingMissing = append(remainingMissing, pkg)
		}
	}
	missing = remainingMissing
	var txErr error
	if len(missing) > 0 {
		if err := a.ensureBlackArchForConfig(cfg); err != nil {
			return fmt.Errorf("enable configured BlackArch repository: %w", err)
		}
		if err := pacman.SyncSystemSettings(cfg.Pacman.ILoveCandy, cfg.Mirrors.Servers, cfg.Mirrors.BlackarchServers); err != nil {
			return err
		}
		if dontUpdate {
			fmt.Fprintf(os.Stdout, "%s Installing %s with pacman -S (-du selected; database refresh/system upgrade skipped).\n", paint("lavender", "→"), strings.Join(missing, ", "))
		} else {
			fmt.Fprintf(os.Stdout, "%s Installing %s with a full system upgrade.\n", paint("mint", "→"), strings.Join(missing, ", "))
		}
		txErr = pacman.New().Install(missing, dontUpdate, cfg.Packages.IgnoreUpdates)
	}
	actual, queryErr := pacman.New().InstalledPackages()
	if queryErr != nil {
		return errors.Join(txErr, fmt.Errorf("could not verify installed package state: %w", queryErr))
	}
	actualSet := makeSet(actual)
	providerCandidates := make([]string, 0, len(packages))
	for _, pkg := range packages {
		if !actualSet[pkg] {
			providerCandidates = append(providerCandidates, pkg)
		}
	}
	providedAfter, providerErr := pacman.New().SatisfiedByInstalledProviders(providerCandidates)
	if providerErr != nil {
		return errors.Join(txErr, fmt.Errorf("could not verify installed package providers: %w", providerErr))
	}
	actualOrProvidedSet := makeSet(actual)
	for _, pkg := range providedAfter {
		actualOrProvidedSet[pkg] = true
	}
	added := []string{}
	for _, pkg := range packages {
		if actualOrProvidedSet[pkg] && !config.Contains(effectiveCfg.Packages.Repo, pkg) {
			cfg.Packages.Repo = append(cfg.Packages.Repo, pkg)
			added = append(added, pkg)
		}
	}
	if len(added) > 0 {
		cfg.Packages.Repo = config.SortedUnique(cfg.Packages.Repo)
		if err := config.WriteAtomic(path, cfg); err != nil {
			return errors.Join(txErr, fmt.Errorf("packages may be installed but config update failed; reconcile with %q: %w", path, err))
		}
		if hasBaseline {
			baseline.ManagedPackages = config.SortedUnique(append(baseline.ManagedPackages, added...))
			baseline.LastSuccessful = time.Now().UTC()
			if err := state.Save("", baseline); err != nil {
				return errors.Join(txErr, fmt.Errorf("config updated but managed state could not be saved: %w", err))
			}
		}
		fmt.Fprintf(os.Stdout, "Added to configuration: %s\n", strings.Join(added, ", "))
	}
	if txErr != nil {
		return fmt.Errorf("Pacman reported an installation failure; verified installed targets were recorded when possible: %w", txErr)
	}
	if len(missing) == 0 {
		fmt.Fprintln(os.Stdout, "All requested packages were already installed.")
	}
	if len(added) == 0 {
		fmt.Fprintln(os.Stdout, "No configuration changes were needed.")
	}
	return nil
}

func (a *App) uninstall(path string, packages []string) error {
	if len(packages) == 0 {
		return errors.New("usage: zacman uninstall <package...>")
	}
	kind, managerPackages, external, parseErr := parseManagerBatch(packages)
	if parseErr != nil {
		return parseErr
	}
	if external {
		return a.uninstallExternal(path, kind, managerPackages)
	}
	packages = config.SortedUnique(packages)
	if err := config.ValidatePackages(packages); err != nil {
		return err
	}
	if err := requireRoot("remove packages and update system configuration"); err != nil {
		return err
	}
	unlock, err := transaction.AcquireLock("")
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := config.LoadBase(path)
	if err != nil {
		return err
	}
	effectiveCfg, err := config.Load(path)
	if err != nil {
		return err
	}
	baseline, hasBaseline, err := state.Load("")
	if err != nil {
		return fmt.Errorf("cannot uninstall while managed state is unreadable: %w", err)
	}
	client := pacman.New()
	installed, err := client.InstalledPackages()
	if err != nil {
		return err
	}
	foreign, err := client.ForeignPackages()
	if err != nil {
		return err
	}
	installedSet := makeSet(installed)
	foreignSet := makeSet(foreign)
	for _, pkg := range packages {
		if installedSet[pkg] && foreignSet[pkg] {
			return fmt.Errorf("%s is installed as a foreign package; Zacman only manages repository packages and will not remove it", pkg)
		}
	}
	managedRemove := []string{}
	for _, pkg := range packages {
		declared := config.Contains(effectiveCfg.Packages.Repo, pkg)
		if !declared {
			if installedSet[pkg] {
				fmt.Fprintf(os.Stderr, "Warning: %s is installed but not managed by Zacman; skipping it.\n", pkg)
			}
			continue
		}
		if installedSet[pkg] {
			managedRemove = append(managedRemove, pkg)
		}
	}
	var txErr error
	if len(managedRemove) > 0 {
		fmt.Fprintf(os.Stdout, "Removing managed packages: %s\n", strings.Join(managedRemove, ", "))
		txErr = pacman.New().Remove(managedRemove)
	}
	actual, queryErr := pacman.New().InstalledPackages()
	if queryErr != nil {
		return errors.Join(txErr, fmt.Errorf("could not verify installed package state: %w", queryErr))
	}
	actualSet := makeSet(actual)
	removed := []string{}
	requested := makeSet(packages)
	for _, pkg := range effectiveCfg.Packages.Repo {
		if requested[pkg] && !actualSet[pkg] {
			removed = append(removed, pkg)
		}
	}
	removed = config.SortedUnique(removed)
	if len(removed) > 0 {
		if _, err := config.RemovePackagesFromReferences(path, removed); err != nil {
			return errors.Join(txErr, fmt.Errorf("package removal completed, but referenced inventory cleanup failed: %w", err))
		}
		cfg.Packages.Repo = removeNamesNoLongerInstalled(cfg.Packages.Repo, makeSet(removed), actualSet)
		cfg.Packages.Foreign.Items = removeNamesNoLongerInstalled(cfg.Packages.Foreign.Items, makeSet(removed), actualSet)
		if err := config.WriteAtomic(path, cfg); err != nil {
			return errors.Join(txErr, fmt.Errorf("package state changed but config update failed; review %s and referenced inventory files: %w", path, err))
		}
		if hasBaseline {
			removedSet := makeSet(removed)
			managed := make([]string, 0, len(baseline.ManagedPackages))
			for _, pkg := range baseline.ManagedPackages {
				if !removedSet[pkg] {
					managed = append(managed, pkg)
				}
			}
			baseline.ManagedPackages = config.SortedUnique(managed)
			baseline.LastSuccessful = time.Now().UTC()
			if err := state.Save("", baseline); err != nil {
				return errors.Join(txErr, fmt.Errorf("config updated but managed state could not be saved: %w", err))
			}
		}
		fmt.Fprintf(os.Stdout, "Removed from configuration: %s\n", strings.Join(removed, ", "))
	}
	if txErr != nil {
		return fmt.Errorf("Pacman reported a removal failure; configuration was reconciled against the packages still installed: %w", txErr)
	}
	if len(removed) == 0 {
		fmt.Fprintln(os.Stdout, "No managed package removals were completed.")
	}
	return nil
}

func removeNamesNoLongerInstalled(source []string, requested, installed map[string]bool) []string {
	kept := make([]string, 0, len(source))
	for _, pkg := range source {
		if requested[pkg] && !installed[pkg] {
			continue
		}
		kept = append(kept, pkg)
	}
	return config.SortedUnique(kept)
}

func (a *App) reconcile(path string, args []string) error {
	dryRun, yes, dontUpdate := false, false, false
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			dryRun = true
		case "--yes", "-y":
			yes = true
		case "-du", "--dont-update":
			dontUpdate = true
		case "--dedupe-config":
			// initCommand has already handled duplicate-list cleanup.
		default:
			return fmt.Errorf("unknown init argument %q", arg)
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	client := pacman.New()
	installed, err := client.InstalledPackages()
	if err != nil {
		return err
	}
	foreign, err := client.ForeignPackages()
	if err != nil {
		return err
	}
	baseline, hasBaseline, err := state.Load("")
	if err != nil {
		return err
	}
	desiredPackages := config.AllPacmanPackages(cfg)
	p := planner.BuildWithRetained(desiredPackages, baseline.ManagedPackages, installed, cfg.Packages.Foreign.Items, hasBaseline)
	p, err = filterInstalledProviderTargets(client, p)
	if err != nil {
		return fmt.Errorf("check installed package providers: %w", err)
	}
	sp := planServices(cfg, p.Install)
	packageIssues := unsupportedInstalledPackages(cfg, foreign)
	settingsChanges, err := pacman.SystemSettingsDiff(cfg.Pacman.ILoveCandy, cfg.Mirrors.Servers, cfg.Mirrors.BlackarchServers)
	if err != nil {
		return err
	}
	extPlan := planExternalPackages(cfg, p.Install)
	systemPlan := buildSystemConfigPlan(cfg, path, hasPendingGreeterThemePackage(cfg, p.Install))
	extraChanges := extPlan.hasChanges() || systemPlan.hasChanges() || len(extPlan.Issues) > 0 || len(systemPlan.issues()) > 0
	printPlan(p, sp, hasBaseline, packageIssues, settingsChanges, false, extraChanges)
	printExternalPlan(extPlan)
	printSystemConfigPlan(systemPlan)
	printCategoryWarnings(cfg)
	if dryRun {
		fmt.Fprintln(os.Stdout, paint("lavender", "Dry run")+": no system, package, config, locale, Pacman settings, or state changes were made.")
		return nil
	}
	if len(sp.issues) > 0 {
		return errors.New("service plan contains unsupported or unavailable units; fix these before applying")
	}
	if len(packageIssues) > 0 {
		return errors.New("configuration contains installed foreign packages; remove them from managed Pacman package sections or manage them outside Zacman")
	}
	if len(extPlan.Issues) > 0 {
		return errors.New("external package plan contains unresolved issues; install the required package-manager binaries and fix the reported errors before applying")
	}
	if len(systemPlan.issues()) > 0 {
		return errors.New("system configuration plan contains unresolved or unsafe paths; fix the reported warnings before applying")
	}
	if err := requireRoot("reconcile package and system configuration"); err != nil {
		return err
	}
	if len(p.Install)+len(p.Remove)+len(sp.changes)+len(settingsChanges) == 0 && !extPlan.hasChanges() && !systemPlan.hasChanges() && hasBaseline {
		fmt.Fprintln(os.Stdout, paint("mint", "✓ System already matches the configuration."))
		return nil
	}
	if !yes {
		ok, err := confirm(paint("lavender", "Apply this configuration? "))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(os.Stdout, "Cancelled; no changes made.")
			return nil
		}
	}
	unlock, err := transaction.AcquireLock("")
	if err != nil {
		return err
	}
	defer unlock()
	// Re-read the declarative source and host state after confirmation. Never
	// apply a changed plan the user did not review.
	currentCfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(currentCfg, cfg) {
		return errors.New("configuration changed while waiting for confirmation; rerun 'zacman diff' and 'zacman init'")
	}
	lockedClient := pacman.New()
	installed, err = lockedClient.InstalledPackages()
	if err != nil {
		return err
	}
	foreign, err = lockedClient.ForeignPackages()
	if err != nil {
		return err
	}
	baseline, hasBaseline, err = state.Load("")
	if err != nil {
		return err
	}
	freshPlan := planner.BuildWithRetained(config.AllPacmanPackages(cfg), baseline.ManagedPackages, installed, cfg.Packages.Foreign.Items, hasBaseline)
	freshPlan, err = filterInstalledProviderTargets(lockedClient, freshPlan)
	if err != nil {
		return fmt.Errorf("recheck installed package providers: %w", err)
	}
	freshServices := planServices(cfg, freshPlan.Install)
	freshPackageIssues := unsupportedInstalledPackages(cfg, foreign)
	freshSettingsChanges, err := pacman.SystemSettingsDiff(cfg.Pacman.ILoveCandy, cfg.Mirrors.Servers, cfg.Mirrors.BlackarchServers)
	if err != nil {
		return err
	}
	freshExternal := planExternalPackages(cfg, freshPlan.Install)
	freshSystem := buildSystemConfigPlan(cfg, path, hasPendingGreeterThemePackage(cfg, freshPlan.Install))
	if !reflect.DeepEqual(freshPlan, p) || !reflect.DeepEqual(freshServices, sp) || !reflect.DeepEqual(freshPackageIssues, packageIssues) || !reflect.DeepEqual(freshSettingsChanges, settingsChanges) || !reflect.DeepEqual(freshExternal, extPlan) || !reflect.DeepEqual(freshSystem, systemPlan) {
		printPlan(freshPlan, freshServices, hasBaseline, freshPackageIssues, freshSettingsChanges, false, freshExternal.hasChanges() || freshSystem.hasChanges() || len(freshExternal.Issues) > 0 || len(freshSystem.issues()) > 0)
		printExternalPlan(freshExternal)
		printSystemConfigPlan(freshSystem)
		return errors.New("system or configuration changed while waiting for confirmation; review the refreshed plan and rerun 'zacman init'")
	}
	if len(freshServices.issues) > 0 || len(freshPackageIssues) > 0 || len(freshExternal.Issues) > 0 || len(freshSystem.issues()) > 0 {
		return errors.New("refreshed plan contains unsupported package or system configuration state; no changes were applied")
	}
	p, sp, settingsChanges, extPlan, systemPlan = freshPlan, freshServices, freshSettingsChanges, freshExternal, freshSystem
	if err := a.ensureBlackArchForConfig(cfg); err != nil {
		return fmt.Errorf("enable configured BlackArch repository: %w", err)
	}
	if err := pacman.SyncSystemSettings(cfg.Pacman.ILoveCandy, cfg.Mirrors.Servers, cfg.Mirrors.BlackarchServers); err != nil {
		return err
	}
	clientForInstall := pacman.New()
	needsPacmanInstall := len(p.Install) > 0 || len(extPlan.AURInstall) > 0
	if needsPacmanInstall {
		if dontUpdate {
			fmt.Fprintln(os.Stdout, paint("lavender", "Install mode:")+" -du selected; skipping pacman -Sy and using current local sync databases (partial-upgrade risk).")
		} else {
			fmt.Fprintln(os.Stdout, paint("cyan", "→")+" Refreshing sync databases only with pacman -Sy (no system upgrade). Run 'zacman update' for a full upgrade.")
			fmt.Fprintln(os.Stdout, paint("yellow", "⚠ Warning:")+" Arch Linux does not support partial upgrades; -Sy followed by target-only installs can cause dependency/library inconsistencies. Use 'zacman update' for a full system upgrade.")
			if err := clientForInstall.SyncDatabases(); err != nil {
				return fmt.Errorf("refresh Pacman sync databases: %w", err)
			}
		}
	}
	if len(p.Install) > 0 {
		if err := a.promoteUnavailableRepoPackagesToAUR(path, &cfg, p.Install, yes, clientForInstall); err != nil {
			return err
		}
		// Moving a target from packages.repo to packages.aur changes the managed
		// Pacman plan. Rebuild all dependent plans from the just-written manifest.
		installedNow, queryErr := clientForInstall.InstalledPackages()
		if queryErr != nil {
			return queryErr
		}
		baselineNow, hasBaselineNow, stateErr := state.Load("")
		if stateErr != nil {
			return stateErr
		}
		p = planner.BuildWithRetained(config.AllPacmanPackages(cfg), baselineNow.ManagedPackages, installedNow, cfg.Packages.Foreign.Items, hasBaselineNow)
		p, err = filterInstalledProviderTargets(clientForInstall, p)
		if err != nil {
			return fmt.Errorf("recheck package providers after AUR migration: %w", err)
		}
		sp = planServices(cfg, p.Install)
		packageIssues = unsupportedInstalledPackages(cfg, foreign)
		extPlan = planExternalPackages(cfg, p.Install)
		systemPlan = buildSystemConfigPlan(cfg, path, hasPendingGreeterThemePackage(cfg, p.Install))
		if len(sp.issues) > 0 || len(packageIssues) > 0 || len(extPlan.Issues) > 0 || len(systemPlan.issues()) > 0 {
			return errors.New("updated package plan contains unresolved package or system configuration issues; no package transaction was started")
		}
	}
	if len(p.Install) > 0 {
		fmt.Fprintln(os.Stdout, paint("mint", "→")+" Installing requested packages only with pacman -S --needed; no system upgrade will run.")
		if err := clientForInstall.InstallSynced(p.Install); err != nil {
			return fmt.Errorf("package installation failed; managed baseline was not advanced; rerun 'zacman init' to reconcile partial changes: %w", err)
		}
	}
	if len(p.Remove) > 0 {
		fmt.Fprintln(os.Stdout, "Removing packages removed from the managed configuration...")
		if err := pacman.New().Remove(p.Remove); err != nil {
			return fmt.Errorf("package removal failed; managed baseline was not advanced; rerun 'zacman init' to reconcile partial changes: %w", err)
		}
	}
	if err := applyExternalPackages(extPlan); err != nil {
		return err
	}
	if err := applySystemConfig(cfg, path, systemPlan); err != nil {
		return fmt.Errorf("package operations may have succeeded, but applying locale/config/greeter settings failed; managed baseline was not advanced: %w", err)
	}
	manager := services.New()
	policy := servicesWithGreeter(cfg)
	for _, requested := range policy.Enable {
		status, err := manager.IsEnabled(requested)
		if err != nil {
			return fmt.Errorf("package changes may have succeeded, but checking service %s failed; state baseline was not advanced: %w", requested, err)
		}
		if status != services.Enabled {
			if status == services.Masked || status == services.Missing || status == services.Static {
				return fmt.Errorf("cannot enable %s: unit is %s; package changes may already have succeeded and state baseline was not advanced", requested, status)
			}
			if err := manager.Enable(requested); err != nil {
				return fmt.Errorf("failed to enable %s; state baseline was not advanced: %w", requested, err)
			}
			fmt.Fprintf(os.Stdout, "Enabled %s\n", requested)
		}
	}
	for _, requested := range policy.Disable {
		status, err := manager.IsEnabled(requested)
		if err != nil {
			return fmt.Errorf("package changes may have succeeded, but checking service %s failed; state baseline was not advanced: %w", requested, err)
		}
		if status != services.Disabled {
			if status == services.Masked || status == services.Missing || status == services.Static {
				return fmt.Errorf("cannot disable %s: unit is %s; package changes may already have succeeded and state baseline was not advanced", requested, status)
			}
			if err := manager.Disable(requested); err != nil {
				return fmt.Errorf("failed to disable %s; state baseline was not advanced: %w", requested, err)
			}
			fmt.Fprintf(os.Stdout, "Disabled %s\n", requested)
		}
	}
	if err := state.Save("", state.State{Version: state.Version, ManagedPackages: config.AllPacmanPackages(cfg)}); err != nil {
		return fmt.Errorf("operations completed but last-successful managed state could not be saved: %w", err)
	}
	fmt.Fprintln(os.Stdout, paint("mint", "✓ Zacman initialized successfully."))
	return nil
}

func (a *App) update(path string, packages []string) error {
	blackarchPackages, isBlackArch, blackarchErr := parseBlackArchBatch(packages)
	if blackarchErr != nil {
		return blackarchErr
	}
	if isBlackArch {
		if err := requireRoot("update BlackArch repository packages"); err != nil {
			return err
		}
		unlock, err := transaction.AcquireLock("")
		if err != nil {
			return err
		}
		enableErr := a.enableBlackArchRepository()
		unlock()
		if enableErr != nil {
			return enableErr
		}
		return a.update(path, blackarchPackages)
	}
	if len(packages) > 0 {
		kind, managerPackages, external, parseErr := parseManagerBatch(packages)
		if parseErr != nil {
			return parseErr
		}
		if external {
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			scope, remote := "user", ""
			if kind == pkgmanager.Flatpak {
				scope, remote = cfg.Flatpak.Scope, cfg.Flatpak.Remote
				if scope == "system" {
					if err := requireRoot("update system-wide Flatpak applications"); err != nil {
						return err
					}
				}
			}
			if err := pkgmanager.Update(kind, managerPackages, remote, scope); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "%s Updated %s package(s) through %s.\n", paint("mint", "✓"), strings.Join(managerPackages, ", "), kind)
			return nil
		}
	}
	packages = config.SortedUnique(packages)
	if err := config.ValidatePackages(packages); err != nil {
		return err
	}
	if err := requireRoot("update packages with Pacman"); err != nil {
		return err
	}
	unlock, err := transaction.AcquireLock("")
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	ignored := makeSet(cfg.Packages.IgnoreUpdates)
	for _, pkg := range packages {
		if ignored[pkg] {
			return fmt.Errorf("%s is listed in packages.ignore_updates; remove it from that list before requesting its update", pkg)
		}
	}
	if err := a.ensureBlackArchForConfig(cfg); err != nil {
		return fmt.Errorf("enable configured BlackArch repository: %w", err)
	}
	if err := pacman.SyncSystemSettings(cfg.Pacman.ILoveCandy, cfg.Mirrors.Servers, cfg.Mirrors.BlackarchServers); err != nil {
		return err
	}
	if len(packages) == 0 {
		fmt.Fprintln(os.Stdout, paint("lavender", "→ Updating the system with pacman -Syu"))
	} else {
		fmt.Fprintf(os.Stdout, "%s Updating the system and requested packages: %s\n", paint("lavender", "→"), strings.Join(packages, ", "))
	}
	if len(cfg.Packages.IgnoreUpdates) > 0 {
		fmt.Fprintf(os.Stdout, "%s Skipping configured update exclusions: %s\n", paint("subtle", "•"), strings.Join(cfg.Packages.IgnoreUpdates, ", "))
	}
	if err := pacman.New().Update(packages, cfg.Packages.IgnoreUpdates); err != nil {
		return fmt.Errorf("Pacman update failed: %w", err)
	}
	fmt.Fprintln(os.Stdout, paint("mint", "✓ Update completed."))
	return nil
}

// promoteUnavailableRepoPackagesToAUR checks only packages explicitly listed in
// the primary [packages].repo array. It never silently reclassifies packages in
// dedicated sections or referenced inventories. AUR candidates are moved into
// [packages.aur] only after the user explicitly approves the AUR build.
func (a *App) promoteUnavailableRepoPackagesToAUR(path string, cfg *config.Config, installTargets []string, autoApprove bool, client pacman.Client) error {
	if len(installTargets) == 0 {
		return nil
	}
	baseCfg, err := config.LoadBase(path)
	if err != nil {
		return err
	}
	repoPackages, err := client.SyncRepositoryPackages()
	if err != nil {
		return err
	}
	repoSet := makeSet(repoPackages)
	mainRepoSet := makeSet(baseCfg.Packages.Repo)
	candidates := []string{}
	unmanagedMissing := []string{}
	for _, pkg := range config.SortedUnique(installTargets) {
		if repoSet[pkg] {
			continue
		}
		if mainRepoSet[pkg] {
			candidates = append(candidates, pkg)
		} else {
			unmanagedMissing = append(unmanagedMissing, pkg)
		}
	}
	if len(unmanagedMissing) > 0 {
		return fmt.Errorf("these Pacman targets are not present in the configured sync repositories: %s; automatic AUR migration is only offered for packages.repo entries", strings.Join(unmanagedMissing, ", "))
	}
	if len(candidates) == 0 {
		return nil
	}
	fmt.Fprintln(os.Stdout, paint("yellow", "Not found in configured sync repositories:")+" "+strings.Join(candidates, ", "))
	if !baseCfg.Packages.AUR.Enabled {
		return fmt.Errorf("AUR lookup skipped because [packages.aur].enabled is false; enable it to allow Zacman to check these names in the AUR: %s", strings.Join(candidates, ", "))
	}
	// Query canonical AUR package names so compatibility aliases (for example,
	// ghgrab -> ghgrab-bin) are resolved without weakening exact-name matching.
	lookupNames := canonicalAURPackages(candidates)
	available, err := lookupAURPackages(lookupNames)
	if err != nil {
		return fmt.Errorf("could not verify missing package names against the AUR; no packages were migrated: %w", err)
	}
	missing := []string{}
	for _, target := range lookupNames {
		if !available[target] {
			missing = append(missing, target)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("these packages are not available in the configured sync repositories or the AUR: %s; check the package names", strings.Join(missing, ", "))
	}
	approved := []string{}
	for _, target := range lookupNames {
		fmt.Fprintf(os.Stdout, "%s %s is available in the AUR. Install it and move it from packages.repo to packages.aur?\n", paint("cyan", "→"), target)
		accept := autoApprove
		if !accept {
			accept, err = confirm(paint("lavender", "Install this AUR package? "))
			if err != nil {
				return err
			}
		}
		if !accept {
			return fmt.Errorf("AUR installation declined for %s; no package source was changed", target)
		}
		approved = append(approved, target)
	}
	if len(approved) == 0 {
		return nil
	}
	approvedSet := makeSet(approved)
	kept := make([]string, 0, len(baseCfg.Packages.Repo))
	for _, pkg := range baseCfg.Packages.Repo {
		if !approvedSet[canonicalAURPackageName(pkg)] {
			kept = append(kept, pkg)
		}
	}
	baseCfg.Packages.Repo = kept
	baseCfg.Packages.AUR.Items = canonicalAURPackages(append(baseCfg.Packages.AUR.Items, approved...))
	if err := config.WriteAtomic(path, baseCfg); err != nil {
		return fmt.Errorf("AUR packages were approved but the manifest could not be updated: %w", err)
	}
	updated, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("manifest was updated for AUR packages but could not be reloaded: %w", err)
	}
	*cfg = updated
	for _, pkg := range approved {
		fmt.Fprintf(os.Stdout, "%s Moved %s from packages.repo to packages.aur.items.\n", paint("mint", "✓"), pkg)
	}
	return nil
}

func unsupportedInstalledPackages(cfg config.Config, foreign []string) []string {
	foreignSet := makeSet(foreign)
	issues := []string{}
	for _, pkg := range cfg.Packages.Repo {
		if foreignSet[pkg] {
			issues = append(issues, fmt.Sprintf("%s is currently installed as a foreign package, not a sync-repository package", pkg))
		}
	}
	sort.Strings(issues)
	return issues
}

func isCriticalUnit(unit string) bool {
	switch unit {
	case "dbus.service", "systemd-journald.service", "systemd-udevd.service", "systemd-logind.service", "NetworkManager.service", "systemd-networkd.service", "systemd-resolved.service", "sshd.service":
		return true
	default:
		return false
	}
}

func (a *App) doctor(path string, checkOnly bool) error {
	failed := false
	for _, binary := range []string{"pacman", "systemctl"} {
		if _, err := exec.LookPath(binary); err != nil {
			fmt.Fprintf(os.Stdout, "[FAIL] %s not found in PATH\n", binary)
			failed = true
		} else {
			fmt.Fprintf(os.Stdout, "[ OK ] %s found\n", binary)
		}
	}

	// A default config created by older Zacman versions may still be root-owned.
	// When launched with sudo from a user session, migrate the config tree to the
	// invoking user so their editor can save atomic replacements without sudo.
	if !checkOnly && os.Geteuid() == 0 && filepath.Clean(path) == filepath.Clean(config.DefaultPath) && strings.TrimSpace(os.Getenv("SUDO_UID")) != "" {
		if err := config.FixOwnership(path); err != nil {
			fmt.Fprintf(os.Stdout, "[FAIL] config ownership: %v\n", err)
			failed = true
		} else {
			fmt.Fprintln(os.Stdout, "[ OK ] config directory and files are owned by the invoking user")
		}
	}

	cfg, err := config.LoadBase(path)
	if err != nil {
		fmt.Fprintf(os.Stdout, "[FAIL] config: %v\n", err)
		failed = true
	} else {
		if checkOnly {
			_, moved := config.OrganizePackages(cfg)
			if len(moved) > 0 {
				for section, pkgs := range moved {
					for _, pkg := range pkgs {
						fmt.Fprintf(os.Stdout, "[WARN] package organization needed in %s: %s\n", section, pkg)
					}
				}
			} else {
				fmt.Fprintln(os.Stdout, "[ OK ] package sections are organized")
			}
		} else {
			organized, moved := config.OrganizePackages(cfg)
			if !reflect.DeepEqual(cfg, organized) {
				if err := config.WriteAtomic(path, organized); err != nil {
					fmt.Fprintf(os.Stdout, "[FAIL] reorganize package sections: %v\n", err)
					fmt.Fprintln(os.Stdout, "       If /etc/zacman is still root-owned, run: sudo zacman doctor")
					failed = true
				} else {
					sections := make([]string, 0, len(moved))
					for section := range moved {
						sections = append(sections, section)
					}
					sort.Strings(sections)
					for _, section := range sections {
						fmt.Fprintf(os.Stdout, "[FIX] organized %s packages: %s\n", section, strings.Join(moved[section], ", "))
					}
				}
			} else {
				fmt.Fprintln(os.Stdout, "[ OK ] package sections are organized")
			}
		}
		if _, err := config.Load(path); err != nil {
			fmt.Fprintf(os.Stdout, "[FAIL] referenced config: %v\n", err)
			failed = true
		} else {
			fmt.Fprintf(os.Stdout, "[ OK ] config %s\n", path)
		}
		if cfg.Packages.AUR.Enabled && len(cfg.Packages.AUR.Items) > 0 {
			fmt.Fprintln(os.Stdout, "[INFO] AUR enabled; Zacman will use yay/paru or bootstrap yay during reconciliation if no helper is installed.")
		}
	}
	if _, _, err := state.Load(""); err != nil {
		fmt.Fprintf(os.Stdout, "[FAIL] state: %v\n", err)
		failed = true
	} else {
		fmt.Fprintln(os.Stdout, "[ OK ] state file readable")
	}
	if os.Geteuid() == 0 {
		fmt.Fprintln(os.Stdout, "[ OK ] running as root")
	} else {
		fmt.Fprintln(os.Stdout, "[INFO] run init/install/uninstall/update with root privileges")
	}
	if failed {
		return errors.New("one or more diagnostics or repairs failed")
	}
	return nil
}
func requireRoot(action string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("root privileges are required to %s; rerun explicitly with sudo", action)
	}
	return nil
}

func confirm(prompt string) (bool, error) {
	fmt.Fprint(os.Stdout, prompt)
	reader := bufio.NewReaderSize(os.Stdin, 1)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "" && err == nil {
		return true, nil // Enter accepts the default action.
	}
	return answer == "y" || answer == "yes", nil
}

// confirmDefaultNo is used for optional adoption of third-party AUR builds.
// Pressing Enter declines; users must explicitly type yes to execute PKGBUILDs.
func confirmDefaultNo(prompt string) (bool, error) {
	fmt.Fprint(os.Stdout, prompt)
	reader := bufio.NewReaderSize(os.Stdin, 1)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func makeSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func yesNo(value bool) string {
	if value {
		return "present"
	}
	return "missing (initial reconciliation is install-only)"
}

func printHelp(w *os.File) {
	fmt.Fprintln(w, paint("lavender", "Zacman")+" — declarative package and systemd manager for Arch Linux")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  init [-dfc|-axp] [--dry-run] [-du]  Reconcile packages and system settings")
	fmt.Fprintln(w, "  diff | status                      Preview changes or inspect state")
	fmt.Fprintln(w, "  config show|validate                Inspect configuration")
	fmt.Fprintln(w, "  install|uninstall PACKAGE...        Manage packages declaratively")
	fmt.Fprintln(w, "  update [PACKAGE...]                 Update system or selected packages")
	fmt.Fprintln(w, "  ctl [list]                          Show service enablement policy")
	fmt.Fprintln(w, "  ctl enable|disable UNIT             Change persistent policy and systemd state")
	fmt.Fprintln(w, "  ctl start|stop|status UNIT          Control or inspect a unit")
	fmt.Fprintln(w, "  doctor                              Check system prerequisites")
	fmt.Fprintln(w, "  completion bash|zsh|fish|powershell Generate shell completion (Bash, Zsh, Fish, PowerShell)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Pacman options are passed through verbatim, e.g. zacman -Ss neovim or zacman -Syu.")
	fmt.Fprintln(w, "Services are listed with `zacman ctl`; init reconciliation keeps its output focused on package and system settings.")
}
