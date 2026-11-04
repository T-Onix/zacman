package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/T-Onix/zacman/internal/config"
	"github.com/T-Onix/zacman/internal/pacman"
	"github.com/T-Onix/zacman/internal/pkgmanager"
	"github.com/T-Onix/zacman/internal/transaction"
)

type externalInstall struct {
	Manager pkgmanager.Kind
	Package string
	Scope   string
	Remote  string
}

type externalPlan struct {
	Install            []externalInstall
	AURInstall         []string
	BootstrapAURHelper bool
	Issues             []string
}

func (p externalPlan) hasChanges() bool {
	return len(p.Install) > 0 || len(p.AURInstall) > 0 || p.BootstrapAURHelper
}

type managerRequest struct {
	kind   pkgmanager.Kind
	label  string
	pkgs   []string
	scope  string
	remote string
}

func managerRequests(cfg config.Config) []managerRequest {
	systemFlatpakLabel := "flatpak (" + cfg.Flatpak.Scope + ")"
	return []managerRequest{
		{kind: pkgmanager.Flatpak, label: systemFlatpakLabel, pkgs: cfg.Flatpak.Pkgs, scope: cfg.Flatpak.Scope, remote: cfg.Flatpak.Remote},
		{kind: pkgmanager.Flatpak, label: "flatpak (user)", pkgs: cfg.Flatpak.UserPkgs, scope: "user", remote: cfg.Flatpak.Remote},
		{kind: pkgmanager.UV, label: "python.uv", pkgs: cfg.Python.UV.Pkgs, scope: "user"},
		{kind: pkgmanager.Pip3, label: "python.pip3", pkgs: cfg.Python.Pip3.Pkgs, scope: "user"},
		{kind: pkgmanager.NPM, label: "node.npm", pkgs: cfg.Node.NPM.Pkgs, scope: "user"},
		{kind: pkgmanager.Cargo, label: "rust.cargo", pkgs: cfg.Rust.Cargo.Pkgs, scope: "user"},
		{kind: pkgmanager.Go, label: "golang", pkgs: cfg.Golang.Pkgs, scope: "user"},
	}
}

func planExternalPackages(cfg config.Config, pacmanInstall ...[]string) externalPlan {
	plan := externalPlan{}
	willInstallManager := map[pkgmanager.Kind]bool{}
	if len(pacmanInstall) > 0 {
		for _, pkg := range pacmanInstall[0] {
			switch pkg {
			case "flatpak":
				willInstallManager[pkgmanager.Flatpak] = true
			case "uv":
				willInstallManager[pkgmanager.UV] = true
			case "python-pip":
				willInstallManager[pkgmanager.Pip3] = true
			case "npm":
				willInstallManager[pkgmanager.NPM] = true
			case "cargo":
				willInstallManager[pkgmanager.Cargo] = true
			case "go":
				willInstallManager[pkgmanager.Go] = true
			}
		}
	}
	for _, request := range managerRequests(cfg) {
		if len(request.pkgs) == 0 {
			continue
		}
		if !pkgmanager.Available(request.kind) {
			if willInstallManager[request.kind] {
				for _, pkg := range request.pkgs {
					plan.Install = append(plan.Install, externalInstall{Manager: request.kind, Package: pkg, Scope: request.scope, Remote: request.remote})
				}
				continue
			}
			plan.Issues = append(plan.Issues, fmt.Sprintf("%s declares packages but the %s executable is not installed; add its Arch provider package to packages.repo first", request.label, request.kind))
			continue
		}
		installed, err := pkgmanager.Installed(request.kind, request.scope, request.remote)
		if err != nil {
			plan.Issues = append(plan.Issues, fmt.Sprintf("cannot inspect %s packages: %v", request.label, err))
			continue
		}
		for _, pkg := range request.pkgs {
			if !pkgmanager.IsInstalled(request.kind, installed, pkg) {
				plan.Install = append(plan.Install, externalInstall{Manager: request.kind, Package: pkg, Scope: request.scope, Remote: request.remote})
			}
		}
	}
	if cfg.Packages.AUR.Enabled && len(cfg.Packages.AUR.Items) > 0 {
		installed, err := pacman.New().InstalledPackages()
		if err != nil {
			plan.Issues = append(plan.Issues, fmt.Sprintf("cannot inspect installed packages for AUR reconciliation: %v", err))
		} else {
			plan.AURInstall = missingAURPackageTargets(cfg.Packages.AUR.Items, installed)
			plan.BootstrapAURHelper = len(plan.AURInstall) > 0 && findAURHelper() == ""
		}
	}
	sort.Slice(plan.Install, func(i, j int) bool {
		if plan.Install[i].Manager != plan.Install[j].Manager {
			return plan.Install[i].Manager < plan.Install[j].Manager
		}
		if plan.Install[i].Scope != plan.Install[j].Scope {
			return plan.Install[i].Scope < plan.Install[j].Scope
		}
		return plan.Install[i].Package < plan.Install[j].Package
	})
	sort.Strings(plan.AURInstall)
	sort.Strings(plan.Issues)
	return plan
}

func printExternalPlan(plan externalPlan) {
	if len(plan.Install) > 0 {
		fmt.Fprintln(os.Stdout, paint("cyan", "External packages to install:"))
		for _, change := range plan.Install {
			label := string(change.Manager)
			if change.Manager == pkgmanager.Flatpak {
				label += " (" + change.Scope + ")"
			}
			fmt.Fprintf(os.Stdout, "  %s %s: %s\n", paint("mint", "+"), label, change.Package)
		}
	}
	if len(plan.AURInstall) > 0 {
		fmt.Fprintln(os.Stdout, paint("cyan", "AUR packages to install:"))
		if plan.BootstrapAURHelper {
			fmt.Fprintln(os.Stdout, "  + bootstrap yay from its AUR PKGBUILD (build runs as the invoking user)")
		}
		for _, name := range plan.AURInstall {
			fmt.Fprintf(os.Stdout, "  %s aur: %s\n", paint("mint", "+"), name)
		}
	}
	if len(plan.Issues) > 0 {
		fmt.Fprintln(os.Stdout, paint("yellow", "External package manager issues:"))
		for _, issue := range plan.Issues {
			fmt.Fprintf(os.Stdout, "  ! %s\n", issue)
		}
	}
}

func applyExternalPackages(plan externalPlan) error {
	if len(plan.Issues) > 0 {
		return fmt.Errorf("external package plan has unresolved issues; install missing package-manager tools or fix the reported errors")
	}
	for _, item := range plan.Install {
		fmt.Fprintf(os.Stdout, "%s Installing %s via %s\n", paint("mint", "→"), item.Package, item.Manager)
		if err := pkgmanager.Install(item.Manager, item.Package, item.Remote, item.Scope); err != nil {
			return fmt.Errorf("external package installation failed after earlier operations may have succeeded; rerun 'zacman diff' and 'zacman init': %w", err)
		}
	}
	if len(plan.AURInstall) > 0 {
		if err := installAURPackagesSynced(plan.AURInstall); err != nil {
			return fmt.Errorf("AUR package installation failed after earlier operations may have succeeded; review 'zacman diff' and rerun 'sudo zacman init': %w", err)
		}
	}
	return nil
}

func printCategoryWarnings(cfg config.Config) {
	for _, warning := range config.CategoryWarnings(cfg) {
		fmt.Fprintf(os.Stdout, "%s %s\n", paint("yellow", "Warning:"), warning)
	}
}

func parseManagerPackageSpec(value string) (pkgmanager.Kind, string, bool, error) {
	index := strings.IndexByte(value, ':')
	if index <= 0 {
		return "", value, false, nil
	}
	prefix, name := strings.ToLower(value[:index]), value[index+1:]
	var kind pkgmanager.Kind
	switch prefix {
	case "flatpak":
		kind = pkgmanager.Flatpak
	case "uv":
		kind = pkgmanager.UV
	case "pip", "pip3":
		kind = pkgmanager.Pip3
	case "npm":
		kind = pkgmanager.NPM
	case "cargo":
		kind = pkgmanager.Cargo
	case "go", "golang":
		kind = pkgmanager.Go
	default:
		return "", value, false, nil
	}
	if err := pkgmanager.ValidatePackage(kind, name); err != nil {
		return "", "", true, err
	}
	return kind, name, true, nil
}

func addManagerPackages(cfg *config.Config, kind pkgmanager.Kind, scope string, packages []string) error {
	switch kind {
	case pkgmanager.Flatpak:
		if scope == "user" {
			cfg.Flatpak.UserPkgs = config.SortedUnique(append(cfg.Flatpak.UserPkgs, packages...))
		} else {
			cfg.Flatpak.Pkgs = config.SortedUnique(append(cfg.Flatpak.Pkgs, packages...))
		}
	case pkgmanager.UV:
		cfg.Python.UV.Pkgs = config.SortedUnique(append(cfg.Python.UV.Pkgs, packages...))
	case pkgmanager.Pip3:
		cfg.Python.Pip3.Pkgs = config.SortedUnique(append(cfg.Python.Pip3.Pkgs, packages...))
	case pkgmanager.NPM:
		cfg.Node.NPM.Pkgs = config.SortedUnique(append(cfg.Node.NPM.Pkgs, packages...))
	case pkgmanager.Cargo:
		cfg.Rust.Cargo.Pkgs = config.SortedUnique(append(cfg.Rust.Cargo.Pkgs, packages...))
	case pkgmanager.Go:
		cfg.Golang.Pkgs = config.SortedUnique(append(cfg.Golang.Pkgs, packages...))
	default:
		return fmt.Errorf("unsupported package manager %q", kind)
	}
	return nil
}

func removeManagerPackages(cfg *config.Config, kind pkgmanager.Kind, scope string, packages []string) {
	remove := func(values []string) []string {
		gone := makeSet(packages)
		kept := []string{}
		for _, v := range values {
			if !gone[v] {
				kept = append(kept, v)
			}
		}
		return config.SortedUnique(kept)
	}
	switch kind {
	case pkgmanager.Flatpak:
		if scope == "user" {
			cfg.Flatpak.UserPkgs = remove(cfg.Flatpak.UserPkgs)
		} else {
			cfg.Flatpak.Pkgs = remove(cfg.Flatpak.Pkgs)
		}
	case pkgmanager.UV:
		cfg.Python.UV.Pkgs = remove(cfg.Python.UV.Pkgs)
	case pkgmanager.Pip3:
		cfg.Python.Pip3.Pkgs = remove(cfg.Python.Pip3.Pkgs)
	case pkgmanager.NPM:
		cfg.Node.NPM.Pkgs = remove(cfg.Node.NPM.Pkgs)
	case pkgmanager.Cargo:
		cfg.Rust.Cargo.Pkgs = remove(cfg.Rust.Cargo.Pkgs)
	case pkgmanager.Go:
		cfg.Golang.Pkgs = remove(cfg.Golang.Pkgs)
	}
}

func parseManagerBatch(values []string) (pkgmanager.Kind, []string, bool, error) {
	if len(values) == 0 {
		return "", nil, false, nil
	}
	firstKind, firstName, isManager, err := parseManagerPackageSpec(values[0])
	if err != nil || !isManager {
		for _, value := range values[1:] {
			_, _, laterManager, laterErr := parseManagerPackageSpec(value)
			if laterErr != nil {
				return "", nil, true, laterErr
			}
			if laterManager {
				return "", nil, true, fmt.Errorf("do not mix Pacman package names and ecosystem package specifiers in one command; run separate commands")
			}
		}
		return "", nil, false, err
	}
	packages := []string{firstName}
	for _, value := range values[1:] {
		kind, name, recognized, parseErr := parseManagerPackageSpec(value)
		if parseErr != nil {
			return "", nil, true, parseErr
		}
		if !recognized || kind != firstKind {
			return "", nil, true, fmt.Errorf("all ecosystem packages in one command must use the same manager prefix (%s:)", firstKind)
		}
		packages = append(packages, name)
	}
	return firstKind, config.SortedUnique(packages), true, nil
}

func managerPackages(cfg config.Config, kind pkgmanager.Kind, scope string) []string {
	switch kind {
	case pkgmanager.Flatpak:
		if scope == "user" {
			return cfg.Flatpak.UserPkgs
		}
		return cfg.Flatpak.Pkgs
	case pkgmanager.UV:
		return cfg.Python.UV.Pkgs
	case pkgmanager.Pip3:
		return cfg.Python.Pip3.Pkgs
	case pkgmanager.NPM:
		return cfg.Node.NPM.Pkgs
	case pkgmanager.Cargo:
		return cfg.Rust.Cargo.Pkgs
	case pkgmanager.Go:
		return cfg.Golang.Pkgs
	default:
		return nil
	}
}

func (a *App) installExternal(path string, kind pkgmanager.Kind, packages []string, dontUpdate bool) error {
	if dontUpdate {
		return errors.New("-du is a Pacman-only option; ecosystem package managers have their own update/install semantics")
	}
	if err := requireRoot("install ecosystem packages and update the Zacman configuration"); err != nil {
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
	effective, err := config.Load(path)
	if err != nil {
		return err
	}
	scope, remote := "user", ""
	if kind == pkgmanager.Flatpak {
		scope, remote = cfg.Flatpak.Scope, cfg.Flatpak.Remote
	}
	if !pkgmanager.Available(kind) {
		return fmt.Errorf("%s is not installed; install its provider package through Pacman first", kind)
	}
	installed, listErr := pkgmanager.Installed(kind, scope, remote)
	if listErr != nil {
		return fmt.Errorf("cannot inspect existing %s packages: %w", kind, listErr)
	}
	succeeded := []string{}
	for _, pkg := range packages {
		if pkgmanager.IsInstalled(kind, installed, pkg) {
			succeeded = append(succeeded, pkg)
			continue
		}
		fmt.Fprintf(os.Stdout, "%s Installing %s via %s\n", paint("mint", "→"), pkg, kind)
		if err := pkgmanager.Install(kind, pkg, remote, scope); err != nil {
			if len(succeeded) > 0 {
				if errPersist := persistExternalInstall(path, &cfg, effective, kind, scope, succeeded); errPersist != nil {
					return fmt.Errorf("%s install failed and earlier packages may be installed; config persistence also failed: %v; install error: %w", kind, errPersist, err)
				}
			}
			return fmt.Errorf("%s install failed; successfully installed earlier packages were recorded when possible: %w", kind, err)
		}
		succeeded = append(succeeded, pkg)
	}
	if err := persistExternalInstall(path, &cfg, effective, kind, scope, succeeded); err != nil {
		return fmt.Errorf("ecosystem package operation succeeded, but updating the Zacman manifest failed: %w", err)
	}
	fmt.Fprintf(os.Stdout, "%s Recorded %d %s package(s) in the Zacman configuration.\n", paint("mint", "✓"), len(succeeded), kind)
	return nil
}

func persistExternalInstall(path string, cfg *config.Config, effective config.Config, kind pkgmanager.Kind, scope string, packages []string) error {
	toRecord := []string{}
	declared := makeSet(managerPackages(effective, kind, scope))
	for _, pkg := range packages {
		if !declared[pkg] {
			toRecord = append(toRecord, pkg)
		}
	}
	if len(toRecord) == 0 {
		return nil
	}
	if err := addManagerPackages(cfg, kind, scope, toRecord); err != nil {
		return err
	}
	return config.WriteAtomic(path, *cfg)
}

func (a *App) uninstallExternal(path string, kind pkgmanager.Kind, packages []string) error {
	if err := requireRoot("remove ecosystem packages and update the Zacman configuration"); err != nil {
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
	scope, remote := "user", ""
	if kind == pkgmanager.Flatpak {
		scope, remote = cfg.Flatpak.Scope, cfg.Flatpak.Remote
	}
	removed := []string{}
	for _, pkg := range packages {
		if err := pkgmanager.Uninstall(kind, pkg, remote, scope); err != nil {
			return fmt.Errorf("%s uninstall may have partially completed; manifest has not yet been changed: %w", kind, err)
		}
		removed = append(removed, pkg)
	}
	removeManagerPackages(&cfg, kind, scope, removed)
	if err := config.WriteAtomic(path, cfg); err != nil {
		return fmt.Errorf("ecosystem packages were removed, but the main manifest could not be updated: %w", err)
	}
	if err := removeManagerFromInventories(path, cfg.Reference.PackageFiles, kind, scope, removed); err != nil {
		return fmt.Errorf("ecosystem packages were removed, but one referenced inventory could not be updated: %w", err)
	}
	fmt.Fprintf(os.Stdout, "%s Removed %d %s package(s) and updated Zacman's config.\n", paint("mint", "✓"), len(removed), kind)
	return nil
}

func removeManagerFromInventories(configPath string, files []string, kind pkgmanager.Kind, scope string, packages []string) error {
	for _, file := range files {
		path := config.ReferenceFilePath(configPath, file)
		inventory, err := config.ReadInventory(path)
		if err != nil {
			return err
		}
		asConfig := config.Default()
		asConfig.Flatpak.Pkgs = inventory.Flatpak
		asConfig.Flatpak.UserPkgs = inventory.FlatpakUser
		asConfig.Python.UV.Pkgs = inventory.PythonUV
		asConfig.Python.Pip3.Pkgs = inventory.PythonPip3
		asConfig.Node.NPM.Pkgs = inventory.NodeNPM
		asConfig.Rust.Cargo.Pkgs = inventory.RustCargo
		asConfig.Golang.Pkgs = inventory.Golang
		removeManagerPackages(&asConfig, kind, scope, packages)
		inventory.Flatpak, inventory.FlatpakUser = asConfig.Flatpak.Pkgs, asConfig.Flatpak.UserPkgs
		inventory.PythonUV, inventory.PythonPip3 = asConfig.Python.UV.Pkgs, asConfig.Python.Pip3.Pkgs
		inventory.NodeNPM, inventory.RustCargo, inventory.Golang = asConfig.Node.NPM.Pkgs, asConfig.Rust.Cargo.Pkgs, asConfig.Golang.Pkgs
		if err := config.WriteFullInventoryAtomic(path, inventory); err != nil {
			return err
		}
	}
	return nil
}
