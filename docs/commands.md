# Commands

## Zacman-managed operations

- `zacman init`: reconcile packages, systemd enablement, Pacman options, and configured mirrors. For missing Pacman/AUR targets it runs `pacman -Sy` once, then installs only requested targets with `pacman -S --needed`; it never upgrades the whole system. Arch Linux does not support partial upgrades, so use `zacman update` regularly and before diagnosing library/package inconsistencies.
- `zacman init -dfc`: create only an empty default `/etc/zacman/zacman.toml`; refuses to overwrite an existing main config and does not modify `/etc/pacman.conf` or create sidecar files.
- `zacman init --dry-run`: display the diff without modifying packages, services, Pacman configuration, mirrors, or state. Duplicate entries are reported but never repaired during a dry run.
- `zacman init -dc`: automatically remove duplicate package names from each TOML package list, keeping the first occurrence and preserving unrelated config/comment formatting, then continue reconciliation. Without `-dc`, `init` prompts before repairing duplicates; that cleanup prompt defaults to No when you press Enter. `-y` also accepts the cleanup.
- When a name in `packages.repo` is absent from all configured sync repositories and `[packages.aur].enabled = true`, Zacman checks the exact name through the AUR RPC. It prompts before installing an exact match, then moves the approved name from `packages.repo` to `packages.aur.items`. Pressing Enter accepts this AUR prompt; type `n` to decline. `-y` accepts confirmations automatically. Zacman does not silently fall back to AUR or rename packages.
- `zacman init -du`: reconcile package membership using `pacman -S` without refreshing databases or upgrading the system. This keeps the existing opt-out behavior and uses the current local sync databases (partial-upgrade risk).
- `zacman update`: run a full system upgrade with `pacman -Syu` and honor `packages.ignore_updates`. This is the only Zacman-managed full-system update path.
- `zacman update PACKAGE...`: run the same full-system upgrade and include the named packages as targets.
- `zacman config show|validate`: inspect/validate TOML.
- `zacman diff|status`: inspect pending changes. Installed package names are not printed as a list.
- `zacman install PACKAGE...`: install repository packages and add successfully installed targets to the manifest.
- `zacman install -du PACKAGE...` or `zacman install PACKAGE... -du`: use `pacman -S` without refresh/full upgrade; this carries a partial-upgrade risk.
- `zacman uninstall PACKAGE...`: remove only Zacman-managed packages and update the manifest based on Pacman's observed result.
- `zacman ctl` / `zacman ctl list`: show the configured service enablement policy.
- `zacman ctl enable UNIT`: enable now and add UNIT to `services.enable`; remove it from `services.disable`.
- `zacman ctl disable UNIT`: disable now and add UNIT to `services.disable`; remove it from `services.enable`.
- `zacman ctl start|stop UNIT`: start/stop now without changing the manifest's enablement policy.
- `zacman ctl status UNIT`: display `systemctl status` for a unit.
- `zacman completion bash|zsh|fish|powershell`: generate self-contained/native shell completion scripts.
- `zacman doctor`: check required commands, configuration, state readability, permissions, and package-section organization. Use `zacman doctor --check` for diagnosis without repair.
- `zacman repo status`: show whether the supported BlackArch repository is enabled.
- `zacman repo enable blackarch`: bootstrap the supported BlackArch repository and keyring after checksum verification.
- `zacman install flatpak:APP_ID`, `uv:TOOL`, `pip3:PACKAGE`, `npm:PACKAGE`, `cargo:CRATE`, or `go:MODULE@VERSION`: use the named ecosystem manager and persist it to the matching TOML section after success.
- `zacman update uv:TOOL`, `pip3:PACKAGE`, `npm:PACKAGE`, `cargo:CRATE`, `go:MODULE`, or `flatpak:APP_ID`: request an ecosystem-specific update through that manager.

## Native Pacman passthrough

Any Pacman option not recognized as a Zacman command is sent directly to Pacman with its original arguments and terminal streams. Examples:

- `zacman -Ss neovim`
- `zacman -Syu`
- `sudo zacman -S neovim`
- `zacman -Qi pacman`
- `zacman -Qm`
- `sudo zacman -Rns PACKAGE`
- `sudo zacman -U ./package.pkg.tar.zst`
- `zacman -h` or `zacman pacman --help`
- `zacman -V` for Pacman's version

This provides the full Pacman CLI without a command allowlist. Native passthrough preserves Pacman's output and exit status but intentionally bypasses Zacman's manifest updates, state tracking, update exclusions, and TOML-driven Pacman/mirror settings. Use Zacman's own `install`, `uninstall`, `update`, and `init` commands when you need its managed behavior.

`--config PATH` overrides the Zacman config path for Zacman commands. Pacman's own options, including its `--config`, are preserved when passing Pacman arguments through. The former `apply` and `plan` commands are removed; use `init` for reconciliation and `diff` for preview.

## Shell completion

Zacman generates native completions for Bash, Zsh, Fish, and PowerShell. Bash output includes its own `_get_comp_words_by_ref` compatibility helper, so it works even when the `bash-completion` package has not been installed or sourced. Completion candidates use the same portable Cobra callbacks across shells, including systemd-unit and package-name suggestions.

### Bash

```bash
mkdir -p ~/.local/share/zacman/completions
zacman completion bash > ~/.local/share/zacman/completions/zacman.bash
```

Add this line to `~/.bashrc`:

```bash
[[ -r "$HOME/.local/share/zacman/completions/zacman.bash" ]] && source "$HOME/.local/share/zacman/completions/zacman.bash"
```

Reload with `source ~/.bashrc`. For just the current session, run `source <(zacman completion bash)`.

### Zsh

```zsh
mkdir -p ~/.zsh/completions
zacman completion zsh > ~/.zsh/completions/_zacman
```

Add `fpath=("$HOME/.zsh/completions" $fpath)` to `~/.zshrc` **before** Oh My Zsh initializes completion (before `source "$ZSH/oh-my-zsh.sh"`). Then run `rm -f ~/.zcompdump*; exec zsh`.

### Fish

```fish
mkdir -p ~/.config/fish/completions
zacman completion fish > ~/.config/fish/completions/zacman.fish
```

Fish loads that file automatically in new sessions. To try immediately, run `zacman completion fish | source`.

### PowerShell

```powershell
zacman completion powershell > "$HOME/zacman-completion.ps1"
```

Add `. "$HOME/zacman-completion.ps1"` to your PowerShell profile, then restart PowerShell. To try immediately, run `zacman completion powershell | Out-String | Invoke-Expression`.

Use `zacman completion <shell> --no-descriptions` to omit descriptions.
