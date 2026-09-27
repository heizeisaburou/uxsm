# uxsm

[![CI](https://github.com/heizeisaburou/uxsm/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/heizeisaburou/uxsm/actions/workflows/ci.yml)

An X11 session manager for `systemd --user`, and the X11 counterpart to
[uwsm](https://github.com/Vladimir-csp/uwsm).

uxsm runs the desktop as a user service, prepares a well-defined session environment, and waits
for a window manager before declaring the graphical session ready. It can also start XDG autostart
entries. When the desktop exits, uxsm stops the whole session and restores the previous user-manager
environment.

The display manager starts uxsm through the selected session entry. The main commands are:

- `uxsm start bspwm.desktop` or `uxsm start -- bspwm` starts a session from an entry or a command.
- `uxsm stop` stops the current uxsm session.
- `uxsm app -- kitty` or `uxsm app firefox.desktop` runs an application in its own unit inside the
  session, like `uwsm app` on Wayland.
- `uxsm check is-active` reports through its exit status whether an uxsm session is running.
- `uxsm finalize` lets a desktop report that it is ready. Most window managers do not need it because
  uxsm detects their EWMH readiness marker.
- `uxsm entry bspwm` generates `bspwm-uxsm.desktop` and can install it in
  `/usr/local/share/xsessions`
- `uxsm check` and `uxsm setup sessions-dir` check and configure the local X11 and Wayland session
  directories used by display managers.

## Documentation

- [Architecture](docs/architecture.md) explains the units, lifecycle, environment, autostart, session
  entries, and display-manager integration.
- [Troubleshooting](docs/troubleshooting.md) covers problems observed on real systems and the relevant
  workarounds.
- [Development](docs/development.md) covers building, testing, packaging, and the source tree.

## Build and install

uxsm requires Go 1.22 or later and uses only the standard library, so Go builds do not need network
access.

`make build` runs `go vet` before compiling. See the
[Go version policy](docs/development.md#go-version) for the reason behind the minimum version.

```sh
make
sudo make install PREFIX=/usr
```

`DESTDIR`, `PREFIX`, and `BINDIR` have their usual meanings. `VERSION` defaults to `git describe`;
pass it explicitly when building outside a Git checkout.

### Without a package manager

Every release also includes a binary tarball, `uxsm-<version>-linux-<arch>.tar.gz`, for machines
without an uxsm package. Its static binary needs neither Go nor a matching glibc:

```sh
tar xzf uxsm-<version>-linux-x86_64.tar.gz
cd uxsm-<version>-linux-x86_64
sudo ./install.sh --prefix /usr
```

The script installs the same files as a package: the binary, systemd user units, and manual page.
`--prefix` defaults to `/usr/local`, `--destdir` prepends a staging root, and `--uninstall` removes
those files. The release's `SHA256SUMS` covers every downloadable file.

## Repository layout

```text
cmd/uxsm/            command dispatch and CLI implementations
internal/            uxsm implementation packages
data/                installed systemd user units
docs/                user and contributor documentation
test/                integration tests, VM orchestration, and distro session data
packaging/arch/      Arch Linux PKGBUILD
packaging/debian/    Debian and Ubuntu packaging
packaging/fedora/    Fedora spec
packaging/opensuse/  openSUSE spec and changes file
packaging/nix/       nixpkgs package
flake.nix            Nix package and NixOS test entry point
.githooks/           Git hooks enabled by `make hooks`
Makefile             shared build and installation interface
```

Code belongs in `internal/` unless it is part of `main`: uxsm is an application, not a Go library.

## Packaging and release checks

All packages except Nix call `make build` and `make install`. When adding an installed file, update
the `Makefile`, every package file list, and the Nix `postInstall` phase.

- **Arch Linux:** `packaging/arch/PKGBUILD`; publish it to the AUR together with the generated
  `.SRCINFO`
- **Debian and Ubuntu:** copy `packaging/debian/` to a top-level `debian/` directory and run
  `dpkg-buildpackage -us -uc`; Ubuntu source packages can be published through a Launchpad PPA.
- **Fedora:** build `packaging/fedora/uxsm.spec` with `rpmbuild` or through COPR.
- **openSUSE:** use `packaging/opensuse/` in OBS. Its changelog belongs in `uxsm.changes`, not the spec.
- **NixOS:** submit `packaging/nix/package.nix` as `pkgs/by-name/ux/uxsm/package.nix` in nixpkgs. In
  this repository, `nix build` builds the current checkout through `flake.nix`.

`make test-vm` builds each selected native package in a VM, installs it in a second clean VM, and
runs the integration suite. `make release` does this for all supported distributions and writes the
artifacts to the `releases/latest` directory. See
[Build and test](docs/development.md#build-and-test).

`make publish TAG=v0.1.0` publishes a version. It checks that the tree is clean and the version is
unused, then creates the tag before building because package versions come from `git describe`.
After testing every distribution, it uploads the packages, both tarballs, and `SHA256SUMS` to a
GitHub release. Add `DRY_RUN=1` to preview every step. See
[Publishing a release](docs/development.md#publishing-a-release).

Packages download the tag archive generated by GitHub, so its checksum cannot be known before the
tag exists there. Afterwards, `test/publish.sh --hashes v0.1.0` downloads that exact archive and
writes its checksum to the Arch and Nix recipes consumed by the AUR and nixpkgs.

## License

[Apache-2.0](LICENSE)
