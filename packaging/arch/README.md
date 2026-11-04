# Arch package recipe

`PKGBUILD` is the repository-maintained Arch package recipe. It builds Zacman from the corresponding Git tag and installs the example TOML only when Pacman first installs the package.

Before preparing an AUR submission:

1. Publish the matching `vX.Y.Z` Git tag.
2. Set `pkgver` in `PKGBUILD` to that tag's version and reset `pkgrel` when required by packaging policy.
3. Run `makepkg --printsrcinfo > .SRCINFO` from this directory and inspect the generated metadata.
4. Build and test the package in a clean Arch build environment with `makepkg --syncdeps --cleanbuild`.
5. Submit only after ensuring the maintainer metadata and package dependencies are correct.

The `SKIP` checksum is used because the source is a Git VCS checkout; the selected Git tag is the source reference. This recipe is not an AUR submission by itself, and the repository does not claim that an AUR package is published.
