# Troubleshooting

## Configuration and state

- **Config missing:** run `sudo zacman init -dfc`. It creates only the main config and refuses to replace an existing file.
- **Config invalid:** inspect it with `zacman config show`, fix the reported field in `/etc/zacman/zacman.toml`, then run `zacman config validate`.
- **Duplicate package entries:** run `sudo zacman init` and approve cleanup, or use `sudo zacman init -dc` to remove duplicates automatically. The first occurrence is retained. `--dry-run` reports duplicates without editing the manifest.
- **Config ownership or comments:** run `sudo zacman doctor` to diagnose the default `/etc/zacman` tree and organize specialist packages. The TOML updater changes affected assignments while attempting to preserve unrelated comments, formatting, and table order.
- **`init` proposes to remove packages you already have:** this may be caused by an old or incomplete managed baseline. Run `sudo zacman init -axp`, inspect `zacman diff`, and only then reconcile. The import appends explicitly installed packages (`pacman -Qqen` and `pacman -Qqem`) to the main manifest, excludes dependency-installed packages, and performs no package transactions.

## Pacman and package transactions

- **Important: partial-upgrade state:** normal `zacman init` uses `pacman -Sy` only when there are missing Pacman/AUR targets, then installs those targets. It does not upgrade the complete system. Arch Linux does not support partial upgrades; run `sudo zacman update` to perform the full `pacman -Syu` upgrade and keep the system coherent.
- **`init -du`:** this skips the `-Sy` sync-database refresh and installs against the local sync databases already present on disk. It is not a partial-upgrade safety feature; stale databases can cause package-resolution problems. The full upgrade command remains `sudo zacman update`.
- **`install -du`:** this uses `pacman -S` without a refresh/full upgrade. It carries a partial-upgrade risk.
- **Partial transaction:** inspect actual package/service state, run `zacman diff`, and rerun the relevant Zacman command after correcting the reported issue. Zacman does not claim to roll back external Pacman/systemd side effects.
- **Pacman lock:** wait for the existing Pacman transaction to finish. Zacman never removes `/var/lib/pacman/db.lck`.
- **Foreign package declaration:** imported foreign packages are inventory, not automatically AUR targets. Move a package to `[packages.aur].items` only when you have verified its actual AUR package name and want Zacman to manage it as an AUR package.
- **AUR package is not found:** confirm `[packages.aur].enabled = true`, check the exact package name in the AUR, and ensure `yay` or `paru` can access the network. AUR fallback during `init` is only offered for eligible names declared in `packages.repo`; it does not guess alternative names.
- **AUR prompt:** pressing Enter accepts the offered AUR installation. Type `n` to decline. `-y` skips confirmations. Review PKGBUILDs before accepting.
- **AUR install fails:** run Zacman with `sudo` from your normal user session so `SUDO_UID` identifies the unprivileged build user. The first bootstrap may need `base-devel`, `git`, and `go`; Zacman builds `yay` as the invoking user. Do not run `makepkg` as root.

## Repositories and mirrors

- **BlackArch setup fails:** run `sudo zacman repo enable blackarch` and inspect the output. Zacman verifies the downloaded upstream `strap.sh` against its pinned SHA-1 checksum and refuses to execute a mismatch. If upstream changes the script/checksum, update the pin only after independently verifying the new official value.
- **BlackArch repository status:** run `zacman repo status` and inspect `/etc/pacman.conf` for the active `[blackarch]` section.
- **Custom mirrors have no effect:** check that the repository includes `/etc/pacman.d/mirrorlist`. General mirrors are written there; BlackArch mirrors use `/etc/pacman.d/zacman-blackarch-mirrorlist` and Zacman's managed Include under `[blackarch]`.
- **Pacman configuration:** Zacman does not create `pacman.conf.example` or insert Pacman template comments into its TOML manifest. Edit `/etc/pacman.conf` directly for unsupported settings such as repository enablement. `[pacman]` manages only documented options such as `ILoveCandy`.

## Desktop, services, and external managers

- **Cursor theme does not update everywhere:** log out and back in after applying the theme so existing Wayland/GTK applications reload Xcursor/environment settings. Zacman does not restart your graphical session.
- **Greeter theme rejected:** automated theme management is SDDM-specific. For a theme under `/usr/share/sddm/themes`, use its directory name or full path. A non-Breeze theme must pass structural checks and an interactive `sddm-greeter --test-mode` preview. On failure or rejection, Zacman selects Breeze when installed or SDDM's embedded fallback. To retry an unchanged rejected theme, remove only the `# Zacman theme verification:` marker line from `/etc/sddm.conf.d/90-zacman-theme.conf`, then rerun `sudo zacman init`.
- **Locale or keyboard operation fails:** check `localectl status`, the requested UTF-8 locale, and `/etc/locale.gen`. Zacman uses `locale-gen` and `localectl`; it reports failures instead of silently claiming success.
- **External package inventory warning during `init -axp`:** if an optional manager query fails, Zacman retains that manager's previous manifest entries and reports a warning. Fix the manager's PATH/configuration and retry the import.
- **Python pip reports an externally managed environment:** Zacman intentionally does not bypass Arch's PEP 668 protection with `--break-system-packages`. Prefer `[python.uv].pkgs` for Python CLI tools or install libraries inside an explicit virtual environment.
- **Go-installed tools are missing from import:** Zacman inspects executable build metadata under the effective `GOBIN` or `GOPATH/bin`. Tools outside those directories or binaries without module metadata cannot be safely inferred.
- **Config files were not imported:** Zacman does not scan arbitrary `~/.config` content because it could capture secrets, caches, or unrelated data. Define explicit `source=>destination` mappings in the appropriate component's `config` array.
- **Ctrl+C during an install:** Zacman forwards terminal interrupts to the child process and returns status 130 when interrupted. If Pacman reports that a transaction is still active, wait for it to finish; never delete its lock file manually.
