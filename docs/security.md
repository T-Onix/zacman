# Security notes

Zacman can perform privileged package and system-configuration operations. Treat its manifest as privileged input even when it is editable by a non-root user.

## Execution model

- Package names, service units, mirror URLs, and configuration mappings are validated before use.
- External commands are invoked with structured argument arrays; TOML values are not interpolated into shell commands.
- Config and state paths are checked against unsafe symlink/path conditions. The transaction lock is opened with Linux `O_NOFOLLOW`, then checked as a regular file before `flock` is applied.
- Individual file replacements are atomic and Zacman preserves unowned lines outside its managed marker regions where supported.
- Package removal is constrained by Zacman's recorded managed baseline; the first reconciliation does not remove undeclared packages.

## Privileged configuration

The default config tree is `/etc/zacman`. Zacman supports an owner-editable config tree, but the directory and files must not be group/world-writable. Do not make `/etc/zacman` writable by every local user. `zacman doctor` can diagnose and repair the default tree's ownership/organization.

## AUR trust boundary

AUR packages are community-maintained PKGBUILDs and may execute arbitrary build-time code as the invoking user. Zacman does not treat AUR contents as trusted or official Arch packages. When an AUR helper is missing, Zacman bootstraps `yay` by cloning its PKGBUILD and building as the invoking unprivileged user, then installs the built package through Pacman. Review every PKGBUILD and its sources before accepting an AUR install.

## BlackArch bootstrap

BlackArch setup downloads its official `strap.sh` over HTTPS, restricts the response size, enforces redirects to the official `blackarch.org` host, and compares the bytes against the SHA-1 checksum pinned in the source before execution. The checksum is the value published by the upstream installation instructions; it is not a detached signature or an independent proof of publisher identity. A mismatch fails closed. The official installer itself modifies Pacman keyring/repository configuration.

## Reporting vulnerabilities

Do not publish exploit details, credentials, or a working attack path in a public issue. Use GitHub's private vulnerability reporting feature when it is enabled for the repository. If it is not enabled, contact the maintainer privately through the GitHub profile linked from the repository and include a minimal reproduction, affected revision, impact, and suggested mitigation. See the repository-level [`SECURITY.md`](../SECURITY.md).
