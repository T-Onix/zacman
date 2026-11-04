# Zacman

[![CI](https://github.com/T-Onix/zacman/actions/workflows/ci.yml/badge.svg)](https://github.com/T-Onix/zacman/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

**Zacman is a declarative system and package manager for Arch Linux, written in Go.** It lets you describe the packages, services, mirrors, and selected desktop/system settings you want, review a proposed plan, and reconcile that state through Arch's native tools.

Zacman does not replace Pacman, systemd, or the package managers for external ecosystems. It coordinates them while keeping a managed package baseline and a human-editable TOML manifest.

## Highlights

- **Declarative package management:** reconcile official repository packages against `/etc/zacman/zacman.toml`, with a first-run install/adopt-only policy.
- **AUR support:** install through `yay` or `paru`; optionally bootstrap `yay` as the invoking non-root user. Missing repository package names can be checked against the AUR and migrated only after confirmation.
- **Pacman compatibility:** pass native Pacman arguments through Zacman without losing Pacman's terminal I/O or exit status.
- **Systemd policy:** manage enabled/disabled service policy separately from runtime start/stop operations.
- **System configuration:** manage Pacman options and mirrors, greeter selection, SDDM themes, cursor themes, locale, keyboard layout, and explicit config-file mappings.
- **Ecosystem integrations:** Flatpak, `uv`, user-scoped `pip`, npm, Cargo, and `go install`.
- **Operational safeguards:** strict TOML validation, duplicate-package repair, atomic file replacement, state-change locking, dry-run planning, and structured command arguments.

## Engineering design

- A layered Go architecture separates CLI orchestration, TOML/config handling, deterministic planning, Pacman operations, service management, state, and external package-manager adapters.
- Configuration updates preserve unrelated TOML content and use atomic file replacement; state-changing operations use an exclusive lock.
- System tools are invoked with structured argument arrays rather than shell-interpolated configuration values.
- Tests use temporary files and mocked command execution; the GitHub Actions workflow is configured to check formatting, run unit/race tests and vet, and build the binary.

## Important Arch Linux behavior

> [!WARNING]
> Arch Linux does not support partial upgrades. When `zacman init` has missing Pacman or AUR targets, it refreshes sync databases with `pacman -Sy` and installs only the requested targets. It does **not** run a full system upgrade. This can leave installed packages out of sync with refreshed repositories. Run `sudo zacman update` regularly and before troubleshooting package/library inconsistencies.

`zacman update` is the Zacman-managed full-upgrade operation and uses `pacman -Syu`. `zacman init -du` skips the database refresh and uses the existing local sync databases; it does not make partial upgrades safe. These behaviors are explicit so reconciliation and full system upgrades remain separate operations.

> [!NOTE]
> Zacman is a pre-1.0 project. Review `zacman diff` before applying changes and test system-level behavior in a disposable Arch Linux VM before relying on it across machines.

## Requirements

- Arch Linux (or an Arch-based system) with Pacman and systemd.
- Root privileges for package transactions, system-wide settings, and system service enablement.
- Go 1.23 or newer to build from source or install with `go install`.
- Optional integrations require their corresponding tools: `yay`/`paru`, Flatpak, `uv`, Python, npm, Cargo, or Go.

## Install

### Install with Go

```bash
go install github.com/T-Onix/zacman@latest
```

Ensure `$(go env GOPATH)/bin` (or `GOBIN`) is on your `PATH`. Go installation does not run privileged post-install hooks. Create the default config explicitly:

```bash
ZACMAN_BIN="$(go env GOBIN)"
if [ -z "$ZACMAN_BIN" ]; then ZACMAN_BIN="$(go env GOPATH)/bin"; fi
sudo "$ZACMAN_BIN/zacman" init -dfc
```

`init -dfc` creates only `/etc/zacman/zacman.toml`. It refuses to overwrite an existing config, installs no packages, and does not modify `/etc/pacman.conf` or create sidecar files.

### Build from a checkout

```bash
git clone https://github.com/T-Onix/zacman.git
cd zacman
make build
sudo make install
```

The Makefile installs the binary under `/usr/local/bin` by default and creates the example config only when the main config does not already exist. Override `PREFIX`, `SYSCONFDIR`, and `DESTDIR` for staged installations.

### Install using the helper script

The repository includes [`scripts/install-from-github.sh`](scripts/install-from-github.sh). It uses `go install` and initializes the default config only if it is missing. Review the script before running it:

```bash
curl -fsSLO https://raw.githubusercontent.com/T-Onix/zacman/main/scripts/install-from-github.sh
less install-from-github.sh
bash install-from-github.sh
```

Tagged binary archives are produced by the release workflow when a `v*` tag is pushed. See [Releasing](CONTRIBUTING.md#release-process) before creating the first release.

## First run

For an existing Arch installation, import explicitly installed packages before asking Zacman to manage the machine:

```bash
sudo zacman init -axp
```

This imports explicitly installed native packages (`pacman -Qqen`) and foreign packages (`pacman -Qqem`) into the main TOML file. Dependency-installed packages are deliberately excluded. It also best-effort inventories supported external package managers and selected system settings. It does not install, remove, or upgrade packages, change services, or create `existing-pkgs.toml` / `pacman.conf.example` sidecar files.

Review and edit the manifest:

```bash
sudo zacman config show
sudo nvim /etc/zacman/zacman.toml
sudo zacman config validate
sudo zacman diff
sudo zacman init --dry-run
sudo zacman init
```

The initial reconciliation is install/adopt-only: Zacman will not remove undeclared packages when it has no prior managed baseline. On later runs, it removes only packages that were part of its previous managed baseline and have since been removed from the effective manifest. If an existing installation has a stale or incomplete baseline, use `sudo zacman init -axp` and inspect `zacman diff` before reconciling.

## Example configuration

Package lists are empty by default. Add only the packages and settings you actually want Zacman to manage:

```toml
schema_version = 1

[packages]
repo = ["git", "neovim", "ripgrep"]
ignore_updates = []

[packages.aur]
enabled = true
items = ["tty-clock"]

[services]
enable = ["NetworkManager.service"]
disable = []

[pacman]
ilove_candy = true
```

The complete commented template is [`examples/zacman.toml`](examples/zacman.toml). See [Configuration](docs/configuration.md) for every supported section, validation rule, and example.

### AUR fallback

When AUR support is enabled and a package listed in `packages.repo` is not present in the configured sync repositories, Zacman checks the exact name using the AUR RPC. If it finds a match, it asks before moving that package from `packages.repo` to `packages.aur.items`. **Pressing Enter accepts the AUR installation**; typing `n` declines it. `-y` accepts confirmations automatically. Zacman does not silently guess alternate package names or silently install AUR build recipes.

Review AUR PKGBUILDs before installing them. AUR packages are community-maintained build recipes, not official Arch repository packages.

## Command overview

| Command | Purpose |
|---|---|
| `zacman init` | Reconcile the manifest with the machine; target-only install behavior |
| `zacman init -dfc` | Create an empty default config without transactions |
| `zacman init -axp` | Import explicitly installed package inventory into the main TOML |
| `zacman init -dc` | Remove duplicate package entries automatically, then reconcile |
| `zacman init -du` | Skip `pacman -Sy` and use local sync databases |
| `zacman diff` | Preview package, service, and settings changes |
| `zacman status` | Summarize managed package/config state |
| `zacman update` | Perform a full system upgrade with `pacman -Syu` |
| `zacman install PKG...` | Install packages and record successful targets |
| `zacman uninstall PKG...` | Remove Zacman-managed repository packages |
| `zacman ctl ...` | Inspect or change systemd service policy/runtime state |
| `zacman doctor` | Diagnose config, state, dependencies, and package organization |
| `zacman completion SHELL` | Generate Bash, Zsh, Fish, or PowerShell completion |
| `zacman repo enable blackarch` | Set up the supported BlackArch repository |

For full options and examples, see [Commands](docs/commands.md).

## Documentation

- [Architecture](docs/architecture.md)
- [Commands and shell completion](docs/commands.md)
- [Configuration reference](docs/configuration.md)
- [Security notes](docs/security.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Contributing and development](CONTRIBUTING.md)
- [Security reporting](SECURITY.md)
- [Changelog](CHANGELOG.md)

## Development

```bash
make fmt
make test
make vet
make build
```

Or run the combined local checks:

```bash
make check
```

The unit tests use temporary files and mocked command execution; they do not transact with the host's real Pacman database. Run real integration checks only in a disposable Arch Linux VM. See [CONTRIBUTING.md](CONTRIBUTING.md) for the development workflow.

## Scope and limitations

- Zacman manages desired package membership, not immutable package versions or snapshots.
- Pacman transactions and systemd operations are separate OS operations; a multi-step reconciliation is not a global atomic transaction and is not automatically rolled back.
- AUR PKGBUILDs and the BlackArch bootstrap script are third-party code. Review the trust boundary and [security notes](docs/security.md).
- Automated greeter theme management is SDDM-specific; other supported greeters can be selected and enabled, but their theme configuration is not automated.
- Zacman does not scan arbitrary files in `~/.config`. Config file copies must be explicitly declared.

## License

Zacman is released under the [MIT License](LICENSE).
