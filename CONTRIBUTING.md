# Contributing

Thanks for considering a contribution to Zacman. Bug reports, focused fixes, tests, and documentation improvements are welcome.

## Before opening an issue

- Check existing issues and discussions for duplicates.
- Include the Zacman version/commit, Arch Linux version, Go version, the exact command, and a sanitized error/output excerpt.
- Never attach `/etc/zacman/zacman.toml`, logs, or command output without checking them for hostnames, usernames, tokens, private paths, and other sensitive data.
- Do not post undisclosed security vulnerabilities publicly; follow [`SECURITY.md`](SECURITY.md).

## Development setup

Requirements: Go 1.23+, Git, and (for system-level integration testing) a disposable Arch Linux VM.

```bash
git clone https://github.com/T-Onix/zacman.git
cd zacman
go mod download
```

## Local checks

Run these before submitting a pull request:

```bash
make fmt
make check
make build
```

`make check` runs formatting checks, unit tests, and `go vet`. The tests use temporary files and mocked process execution. They must not modify the host's real Pacman database or systemd configuration. Any test that needs real Pacman/systemd behavior belongs in a disposable VM and must be explicitly opt-in.

## Contribution guidelines

- Keep implementation packages under `internal/` unless there is a deliberate decision to expose a stable public Go API.
- Prefer structured argument arrays over shell command strings. Never execute TOML values through a shell.
- Keep Pacman as the authority for package dependency resolution and system transactions.
- Preserve unknown/unowned configuration content when editing system files or TOML.
- Add tests for behavior changes, especially planning, state transitions, path validation, command arguments, and error handling.
- Document user-visible flags and behavior in `docs/commands.md`, configuration changes in `docs/configuration.md`, and security-relevant changes in `docs/security.md`.
- Do not weaken the first-run install/adopt-only policy or expand package removal beyond the previous Zacman-managed baseline without an explicit design discussion and tests.
- Make partial-upgrade risks clear anywhere a command refreshes databases without upgrading the full system.

## Pull requests

Keep pull requests focused. Describe the problem, the design choice, behavior changes, tests run, and any migration or risk. Include terminal output only after redacting sensitive details.

## Release process

Releases are created from signed-off commits on the default branch:

1. Update `CHANGELOG.md` and confirm the documentation matches the CLI.
2. Run `make check` and `make build` on a current Go toolchain.
3. Update the version in `packaging/arch/PKGBUILD`; generate `.SRCINFO` with `makepkg --printsrcinfo` when preparing an AUR submission.
4. Create and push an annotated version tag, for example `v0.1.0`:

   ```bash
   git tag -a v0.1.0 -m "Zacman v0.1.0"
   git push origin v0.1.0
   ```

5. The `release.yml` workflow uses GoReleaser to build Linux amd64/arm64 archives and publish a GitHub Release with SHA-256 checksums.
6. Verify the published assets and checksums before updating installation/package instructions.

Do not create a tag until the CI workflow passes on the exact commit being released.
