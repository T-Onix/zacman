# Configuration

Default path: `/etc/zacman/zacman.toml`.

```toml
schema_version = 1

[packages]
repo = []
ignore_updates = []

[packages.aur]
enabled = false
items = [
  # "paru",
  # "yay",
]

[services]
enable = []
disable = []

[mirrors]
servers = []
blackarch_servers = []

[pacman]
ilove_candy = true
```

`packages.repo` lists official repository packages that Zacman should manage. It intentionally defaults to an empty list. `packages.ignore_updates` lists package names passed to `pacman --ignore` during update transactions. When `[packages.aur].enabled = true`, names under `items` are installed with `yay` or `paru`. During `init`, if a `packages.repo` target is absent from the refreshed sync repositories, Zacman checks its exact name through the AUR RPC and prompts before migrating an available match into `packages.aur.items`. The AUR prompt accepts on Enter; type `n` to decline, or use `-y` to accept confirmations automatically. If neither AUR helper is present, Zacman bootstraps `yay` from its AUR PKGBUILD, running the clone/build as the invoking unprivileged user and installing the resulting package with Pacman. AUR PKGBUILDs are third-party code; review them before installing.

`services.enable` and `services.disable` declare systemd enabled/disabled policy enforced by `init`; reconciliation does not start or stop units. `zacman ctl enable/disable` updates the policy and immediately applies the corresponding systemd action, while `zacman ctl start/stop` changes runtime state without changing the manifest. Mirror `servers` are inserted into `/etc/pacman.d/mirrorlist`; use URLs compatible with all repositories including that file. `blackarch_servers` are written to a separate managed mirrorlist and included under `[blackarch]`. If the repository is not active, Zacman uses the official BlackArch strap script only for explicit `repo enable`, a `blackarch:` package request, or after a confirmed reconcile when BlackArch mirrors are configured; the script is downloaded over HTTPS and checksum-verified first. `pacman.ilove_candy` manages the Pacman `ILoveCandy` option in `[options]` and defaults to true.

Zacman writes its own marker blocks in system configuration/mirrorlist files and preserves unowned lines. Each file replacement is atomic, although editing several system files is not a single atomic transaction.

## Pacman transaction modes

When `init` has missing repository/AUR targets, it refreshes sync databases with `pacman -Sy` and then installs only those targets; it does not upgrade the whole system. Arch Linux does not support partial upgrades, so run `sudo zacman update` regularly and before troubleshooting package/library inconsistencies. `init -du` skips the database refresh and uses the local sync databases already on disk. This flag does not make partial upgrades safe. `update` is the full-system path and uses `pacman -Syu`.


## Existing package inventory

Run `sudo zacman init -axp` to import only explicitly installed packages: native repository packages from `pacman -Qqen`, and explicitly installed foreign packages from `pacman -Qqem`. Packages whose Pacman install reason is “dependency” are excluded. For example, if Neovim and its libraries are installed, Zacman records Neovim but does not record dependency-installed libraries as desired packages. Pacman still installs and resolves dependencies normally for declared targets. The command appends the discovered package and manager inventory to the appropriate sections of `/etc/zacman/zacman.toml`, preserving existing entries, settings, and comments. It creates no `existing-pkgs.toml`, `user-packages.toml`, or Pacman template file.



Referenced inventories remain supported for backward compatibility and for users who explicitly maintain their own files; they are never created or automatically referenced by a new config or by `init -axp`. Explicitly referenced entries participate in planning and reconciliation. The import command resets Zacman's managed baseline to the current explicitly installed repository-package set and performs no Pacman transactions. Dependency-installed packages remain installed but are not included in the desired package inventory.


## Component, greeter, locale, and ecosystem sections

Pacman remains responsible for Arch repository packages. The following sections keep specialized software and non-Pacman tools from being mixed into `packages.repo`:

```toml
[flatpak]
pkgs = ["org.gimp.GIMP"]            # system installation
user_pkgs = ["com.spotify.Client"] # user installation
remote = "flathub"
scope = "system"                    # default scope for pkgs: system or user

[greeter]
pkgs = []
system = "sddm"                     # sddm, gdm, lightdm, greetd, ly, emptty
# SDDM themes may be selected from an already installed local theme:
theme = "breeze"
theme_source = "local"              # or "repo"
theme_package = ""                  # required with theme_source = "repo"

[mouse]
pkgs = ["breeze-cursors"]          # optional Pacman cursor-theme packages
theme = "Breeze_Light"             # installed theme name or absolute path under an icon-theme root
theme_source = "local"             # or "repo"
theme_package = ""                  # required with theme_source = "repo"
size = 32                           # 0 preserves current size; otherwise 8-128 px

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
keyboard_variant = ""
keyboard_model = ""
keyboard_options = ""

[python.uv]
pkgs = ["ruff"]
config = []

[python.pip3]
pkgs = ["httpx"]
config = []

[node.npm]
pkgs = ["eslint", "@types/node"]
config = []

[rust.cargo]
pkgs = ["bat"]
config = []

[golang]
pkgs = ["github.com/example/tool@latest"]
config = []
```

`pkgs` in `desktop`, `audio`, `kernel`, `fonts`, and `greeter` are ordinary Pacman package names. Their category is kept separate for readable manifests and advisory checks, but installation and dependency resolution still use Pacman. `config` entries are explicit `source=>destination` copies: each source must be a regular file under `/etc/zacman` (for example, `profiles/niri/config.kdl`), and each destination must be under `/etc/` or `~/.config/`. Zacman makes a `.zacman-backup` beside an existing destination before replacing it. It never executes text in a config mapping as a command.

`greeter.system` adds the selected greeter's systemd unit to the enable policy. Automated theme application is currently SDDM-specific: `theme` accepts either a directory name or an absolute path beneath `/usr/share/sddm/themes`; `theme_source = "local"` requires that directory to exist. Zacman resolves an absolute path to its final directory name when writing SDDM’s `Current` setting, while `theme_source = "repo"` requires `theme_package` to identify the Pacman package that supplies it. Before activating a non-Breeze theme, Zacman validates `metadata.desktop` and the declared `MainScript`, launches `sddm-greeter --test-mode` in the logged-in user session, and asks whether the preview rendered correctly. On failure, no graphical session, or rejection, it selects Breeze if installed or leaves `Current` empty to use SDDM’s embedded fallback. A verification marker prevents repeatedly retrying the same unchanged failed theme; changing theme files changes the fingerprint and enables a fresh preview. Other greeters can be selected/installed/enabled, but custom theme management is not yet automated for them.

`mouse.pkgs` installs cursor-related packages with Pacman. `mouse.theme` accepts an installed cursor-theme directory name (for example `Breeze_Light`) or an absolute path below `/usr/share/icons`, `/usr/local/share/icons`, `~/.local/share/icons`, or `~/.icons`. Set `theme_source = "local"` for an already-installed theme, or `theme_source = "repo"` plus `theme_package` when the theme package must be installed. Cursor themes must contain a non-empty `cursors/` directory; symlinked theme paths are rejected. `mouse.size` optionally sets the cursor size in pixels (8–128; `0` preserves the current size). Zacman updates KDE/Plasma's `kcminputrc`, GTK 3/4 and GTK 2 settings, the freedesktop `~/.local/share/icons/default/index.theme` fallback (and reads the legacy `~/.icons/default/index.theme` fallback when detecting existing settings), and a per-user `environment.d` file. Changes to an already-running Wayland session may require logging out and back in; Zacman does not restart the desktop session. The active display manager greeter theme and the logged-in user's cursor theme are separate settings.

`locale.system_language` should be a UTF-8 locale such as `en_US.UTF-8`. Zacman enables the corresponding entry in `/etc/locale.gen`, runs `locale-gen`, then applies the `LANG` setting through `localectl`. Keyboard fields map to `localectl set-x11-keymap`. On systems without functional systemd-localed/localectl, these changes fail with an actionable error rather than being reported successful.

External ecosystem packages are installed through their native package managers, never through a shell:

- `[flatpak].pkgs` / `[flatpak].user_pkgs`: Flatpak application IDs from the configured `remote`.
- `[python.uv].pkgs`: `uv tool install`; preferred for CLI tools.
- `[python.pip3].pkgs`: user-scoped `python3 -m pip install --user`; Zacman does not bypass Arch's PEP 668 guard.
- `[node.npm].pkgs`: global npm packages (including scoped names such as `@types/node`).
- `[rust.cargo].pkgs`: Cargo-installed crates; use `crate@version` to request a version.
- `[golang].pkgs`: Go modules installed with `go install`, preferably with `@version` or `@latest`.

Run `zacman install flatpak:org.gimp.GIMP`, `zacman install uv:ruff`, `zacman install pip3:httpx`, `zacman install npm:@types/node`, `zacman install cargo:bat`, or `zacman install go:github.com/example/tool@latest` for single-command installs that update the manifest after success. Native ecosystem commands are used; if an install succeeds but manifest persistence fails, Zacman reports that discrepancy. `go install` tools do not have a universal safe uninstall mapping, so Zacman refuses to guess which executable should be deleted.

`zacman init -axp` inventories installed Pacman packages, classifies recognizable kernel/audio/desktop/font/greeter packages, and best-effort queries Flatpak, uv, user pip, global npm, Cargo, Go-installed binary module metadata, system locale, keyboard layout, display manager, and SDDM theme. When an optional manager query fails it preserves the old inventory for that manager and warns. It cannot safely infer every arbitrary local config file and therefore preserves any explicitly configured `config` mappings rather than scanning and overwriting every file in `~/.config`. Package category classification is heuristic; `zacman diff` prints warnings when a likely specialist package sits in `packages.repo`, especially when the same name is also listed in a dedicated section.


## BlackArch repository setup

Use `sudo zacman repo enable blackarch` to bootstrap the repository and signing keyring using BlackArch's documented `strap.sh` path. `sudo zacman install blackarch:commix` will enable the repository if needed and then install `commix`; the `blackarch:` prefix is required when you want Zacman to infer that a target belongs to BlackArch. Zacman verifies the downloaded script against the SHA-1 checksum currently published by BlackArch before executing it. This official checksum is pinned in the source, so if BlackArch changes the script/checksum, Zacman will fail closed until its pin is updated.
