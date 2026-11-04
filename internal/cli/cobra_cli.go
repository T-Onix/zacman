package cli

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/T-Onix/zacman/internal/config"
	"github.com/spf13/cobra"
)

func (a *App) newCobraRoot() *cobra.Command {
	configPath := config.DefaultPath
	noColor := false
	root := &cobra.Command{
		Use:              "zacman",
		Short:            "Declarative package and systemd manager for Arch Linux",
		Long:             "Zacman manages desired package membership and systemd enablement through native Pacman and systemctl.",
		Version:          a.version,
		SilenceErrors:    true,
		SilenceUsage:     true,
		TraverseChildren: true,
	}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	// Register Cobra's built-in help flags once. The shorthand -h is an alias
	// for --help, not a second independent flag.
	root.InitDefaultHelpFlag()
	// Use our own completion command so generated Bash completion is self-contained
	// and does not fail when bash-completion is not installed or sourced.
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().StringVar(&configPath, "config", config.DefaultPath, "Path to the Zacman TOML configuration")
	root.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable Zacman terminal colors")
	root.SetVersionTemplate("zacman {{.Version}}\n")
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error {
		noColorOverride = noColor
		if strings.TrimSpace(configPath) == "" {
			return fmt.Errorf("--config path cannot be empty")
		}
		return nil
	}

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Reconcile the system with the Zacman configuration",
		Long:  "Reconcile configured packages and systemd startup policy. Missing repository/AUR targets trigger a sync-database refresh with pacman -Sy followed by target-only installs; init never runs a full system upgrade. Arch Linux does not support partial upgrades, so use zacman update for a full upgrade. Duplicate package entries prompt for cleanup; -dc removes duplicates without prompting. Use --default-file-config to create a fresh empty configuration, or --add-existing-packages to import the installed package inventory without package transactions.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			modeArgs := append([]string(nil), args...)
			if value, _ := cmd.Flags().GetBool("default-file-config"); value {
				modeArgs = append(modeArgs, "--default-file-config")
			}
			if value, _ := cmd.Flags().GetBool("add-existing-packages"); value {
				modeArgs = append(modeArgs, "--add-existing-packages")
			}
			if value, _ := cmd.Flags().GetBool("dry-run"); value {
				modeArgs = append(modeArgs, "--dry-run")
			}
			if value, _ := cmd.Flags().GetBool("yes"); value {
				modeArgs = append(modeArgs, "--yes")
			}
			if value, _ := cmd.Flags().GetBool("dont-update"); value {
				modeArgs = append(modeArgs, "-du")
			}
			if value, _ := cmd.Flags().GetBool("dedupe-config"); value {
				modeArgs = append(modeArgs, "--dedupe-config")
			}
			return a.initCommand(configPath, modeArgs)
		},
	}
	initCmd.Flags().Bool("default-file-config", false, "Create only an empty default zacman.toml; never overwrite an existing config")
	initCmd.Flags().Bool("add-existing-packages", false, "Import explicitly installed packages only; exclude dependencies and run no package transactions")
	initCmd.Flags().Bool("dry-run", false, "Preview reconciliation without changes")
	initCmd.Flags().BoolP("yes", "y", false, "Skip confirmation")
	initCmd.Flags().Bool("dont-update", false, "Use -du/--dont-update: install with pacman -S without refreshing databases (partial-upgrade risk)")
	initCmd.Flags().Bool("dedupe-config", false, "Use -dc to remove duplicate package entries without prompting")
	// Cobra uses one help flag with a shorthand alias on each command.
	initCmd.InitDefaultHelpFlag()
	root.AddCommand(initCmd)

	configCmd := &cobra.Command{Use: "config", Short: "Inspect and validate configuration"}
	configCmd.AddCommand(&cobra.Command{
		Use: "show", Short: "Print the primary TOML file", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, args []string) error { return a.configCommand(configPath, []string{"show"}) },
	})
	configCmd.AddCommand(&cobra.Command{
		Use: "validate", Short: "Validate the primary and referenced TOML files", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, args []string) error { return a.configCommand(configPath, []string{"validate"}) },
	})
	root.AddCommand(configCmd)

	root.AddCommand(&cobra.Command{
		Use: "diff", Short: "Show pending package, service, and settings changes", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return a.showDiffOrStatus(configPath, "diff") },
	})
	root.AddCommand(&cobra.Command{
		Use: "status", Short: "Summarize current package and configuration state", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return a.showDiffOrStatus(configPath, "status") },
	})
	installCmd := &cobra.Command{
		Use: "install <package...>", Short: "Install repository packages and record them in the manifest", Args: cobra.MinimumNArgs(1),
		ValidArgsFunction: packageCompletion(false),
		RunE: func(cmd *cobra.Command, args []string) error {
			dontUpdate, _ := cmd.Flags().GetBool("dont-update")
			return a.install(configPath, args, dontUpdate)
		},
	}
	installCmd.Flags().Bool("dont-update", false, "Use -du/--dont-update: pacman -S without refreshing databases or upgrading the system (partial-upgrade risk)")
	installCmd.Flags().SetInterspersed(true)
	root.AddCommand(installCmd)
	root.AddCommand(&cobra.Command{
		Use: "uninstall <package...>", Aliases: []string{"remove", "rm"}, Short: "Remove managed repository packages and update the manifest", Args: cobra.MinimumNArgs(1),
		ValidArgsFunction: packageCompletion(true),
		RunE:              func(_ *cobra.Command, args []string) error { return a.uninstall(configPath, args) },
	})
	root.AddCommand(&cobra.Command{
		Use: "update [package...]", Short: "Perform a full system update or target selected packages", Args: cobra.ArbitraryArgs,
		ValidArgsFunction: packageCompletion(false),
		RunE:              func(_ *cobra.Command, args []string) error { return a.update(configPath, args) },
	})
	doctorCmd := &cobra.Command{
		Use: "doctor", Short: "Diagnose and repair Zacman configuration organization and permissions", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			checkOnly, _ := cmd.Flags().GetBool("check")
			return a.doctor(configPath, checkOnly)
		},
	}
	doctorCmd.Flags().Bool("check", false, "Only diagnose; do not reorganize package sections or adjust ownership")
	root.AddCommand(doctorCmd)
	root.AddCommand(a.newCtlCommand(configPath))
	root.AddCommand(a.newRepoCommand())
	root.AddCommand(newCompletionCommand())

	return root
}

func (a *App) newCtlCommand(configPath string) *cobra.Command {
	ctl := &cobra.Command{
		Use:   "ctl",
		Short: "Inspect and control systemd units",
		Long:  "enable/disable update the persistent services.enable/services.disable policy and invoke systemctl immediately. start/stop are runtime-only and do not change the TOML policy.",
		Args:  cobra.NoArgs,
		RunE:  func(_ *cobra.Command, _ []string) error { return a.showControlConfig(configPath) },
	}
	ctl.AddCommand(&cobra.Command{
		Use: "list", Short: "Show configured service startup policies", Aliases: []string{"ls"}, Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return a.showControlConfig(configPath) },
	})
	for _, action := range []string{"enable", "disable", "start", "stop", "status"} {
		action := action
		cmd := &cobra.Command{
			Use:               action + " <unit>",
			Short:             ctlDescription(action),
			Args:              cobra.ExactArgs(1),
			ValidArgsFunction: systemUnitCompletion,
			RunE: func(_ *cobra.Command, args []string) error {
				if action == "enable" || action == "disable" {
					return a.controlPolicy(configPath, action, args[0])
				}
				return a.controlRuntime(action, args[0])
			},
		}
		ctl.AddCommand(cmd)
	}
	return ctl
}

func ctlDescription(action string) string {
	switch action {
	case "enable":
		return "Enable a unit and add it to the persistent enable policy"
	case "disable":
		return "Disable a unit and add it to the persistent disable policy"
	case "start":
		return "Start a unit now without changing startup policy"
	case "stop":
		return "Stop a unit now without changing startup policy"
	case "status":
		return "Show detailed systemctl status for a unit"
	default:
		return "Control a systemd unit"
	}
}

func systemUnitCompletion(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	out, err := exec.Command("systemctl", "list-unit-files", "--no-legend", "--no-pager").Output()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	result := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		unit := fields[0]
		if strings.HasPrefix(unit, toComplete) && !seen[unit] && config.ValidateServiceUnit(unit) == nil {
			seen[unit] = true
			result = append(result, unit)
		}
	}
	sort.Strings(result)
	return result, cobra.ShellCompDirectiveNoFileComp
}

func packageCompletion(installedOnly bool) cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var command *exec.Cmd
		if installedOnly {
			command = exec.Command("pacman", "-Qq")
		} else {
			command = exec.Command("pacman", "-Slq")
		}
		out, err := command.Output()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		result := []string{}
		if !installedOnly {
			for _, prefix := range []string{"flatpak:", "uv:", "pip3:", "npm:", "cargo:", "go:", "aur:", "blackarch:"} {
				if strings.HasPrefix(prefix, toComplete) {
					result = append(result, prefix)
				}
			}
		}
		for _, line := range strings.Split(string(out), "\n") {
			name := strings.TrimSpace(line)
			if name != "" && strings.HasPrefix(name, toComplete) {
				result = append(result, name)
			}
		}
		sort.Strings(result)
		return result, cobra.ShellCompDirectiveNoFileComp
	}
}
