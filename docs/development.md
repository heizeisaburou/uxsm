# Developing uxsm

This document is for contributors and package maintainers. For runtime behavior, see [Architecture](architecture.md). For user-facing failure diagnosis, see [Troubleshooting](troubleshooting.md).

## Build and test

![Build, test, and release paths](flows/build-and-tests.svg)

[DOT source](flows/build-and-tests.dot)

The important distinction is the boundary each command tests:

| Command | Where it runs | What it proves |
| --- | --- | --- |
| `make test` | Current system, without installing uxsm | Isolated Go behavior. |
| `make test-vm` | Disposable VMs | The installed native package with X11 and `systemd --user`. |

`make test-vm` is not the unit suite on another distribution. It builds the native package in one VM, installs it in a second clean VM, and exercises complete sessions there.

uxsm uses only the Go standard library. Common commands are:

```sh
make build             # go vet, then bin/uxsm
make test              # Go unit tests
make check             # formatting, vet, unit tests, and shell syntax
make test-vm           # Ubuntu by default
make test-vm DISTROS=pair
make test-vm DISTROS=all
make test-vm FAST=1    # build and test in one VM
make test-nixos        # NixOS VM declared by the flake
make release           # strict VM path for every distribution
```

### Unit tests

`go test ./...` covers behavior that does not require privileges or a real graphical session:

- command dispatch and entry-versus-command parsing;
- XDG lookup and parsing of `.desktop` entries, including `Exec=` quoting;
- session identity, runtime files, and environment-file loading;
- environment changes and exact restoration;
- session-entry generation and round trips;
- LightDM, SDDM, and GDM configuration on a fake filesystem;
- unit names, session-bus detection, and `pidfd` waiting.

### Integration tests

[`test/release.sh`](../test/release.sh) archives the current working tree, including uncommitted changes, and drives disposable QEMU machines through [`test/vm.sh`](../test/vm.sh). Each distribution uses two VMs:

1. a build VM creates the native package with the real recipe under `packaging/`; and
2. a clean VM installs that package through the distribution package manager and runs the suite.

The scripts under `test/integration` use Xvfb and normally replace the display manager with a transient service. The last test installs and starts LightDM instead.

| Script | Coverage |
| --- | --- |
| `01-desktop-service.sh` | Entry and direct-command startup; the desktop is the service's main process. |
| `02-session-shutdown.sh` | Desktop exit, login-process death, and `uxsm stop`. |
| `03-session-identity.sh` | `DesktopNames=`, `-D`, `-e`, and XDG variables. |
| `04-session-environment.sh` | `env*` loading and exact restoration through all shutdown paths. |
| `05-generated-entries.sh` | Installation, overwrite protection, `check`, and `setup`. |
| `06-session-ready.sh` | Both readiness signals and the timeout path. |
| `07-xdg-autostart.sh` | Autostart, `--no-autostart`, generated entries, and application slices. |
| `08-display-manager.sh` | A real LightDM session with autologin over Xvfb. |
| `09-app.sh` | `uxsm app` units, slices, entries, actions, properties, and shutdown. |

The supported matrix is Ubuntu 24.04, Debian 13, Arch Linux, Fedora 43, and openSUSE Tumbleweed. `DISTROS=quick` selects Ubuntu, `pair` selects Ubuntu and Arch, and `all` selects all five.

With `FAST=1`, one VM builds and tests the package. On Ubuntu this reduced a measured run from 4:07 to 3:37, but it cannot detect undeclared runtime dependencies because build dependencies remain installed. Use it while developing, not when publishing. `make release` always uses separate build and test VMs.

### NixOS

`make test-nixos` starts a complete declarative NixOS machine with LightDM, autologin, bspwm, and an uxsm session entry. It verifies that the session starts, the desktop is the service's main process, the XDG identity is present, and stopping the display manager tears everything down.

The definition is [`test/nixos/session.nix`](../test/nixos/session.nix) and is exposed through the flake's `checks`. It requires Nix and a running Nix daemon, just as the other VM tests require QEMU. `flake.lock` pins nixpkgs so the test machine remains reproducible. This test covers the NixOS layout and packaging path, which differ from conventional distributions.

### Continuous integration and hooks

[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) runs `make check` and `make build` on every push and pull request. The VM suite is available through `workflow_dispatch`, with the distribution set as an input. GitHub-hosted runners expose `/dev/kvm`; a full two-VM distribution run takes about four minutes there.

The pre-commit hook runs `gofmt`, `go vet`, unit tests, and `sh -n` over shell scripts. The pre-push hook requires a successful `make release` for the exact commit pushed to `main`, `master`, or a `vX.Y.Z` tag. Enable both with:

```sh
make hooks
```

## Source map

| Path | Role |
| --- | --- |
| `cmd/uxsm` | Public CLI and internal subcommands called by systemd units. |
| `internal/desktopentry` | XDG lookup and desktop-entry parsing. |
| `internal/session` | Session identity and runtime exchange files. |
| `internal/sessionenv` | Environment preparation and restoration. |
| `internal/sessionentry` | Known desktop table and entry generation. |
| `internal/systemd` | `systemd --user` and D-Bus operations. |
| `internal/dm` | Display-manager detection and configuration. |
| `internal/appunit` | Unit names, slices, and commands for `uxsm app`. |
| `internal/autostart` | Runtime drop-in that moves XDG autostart into the session slice. |
| `internal/pidwait` | `pidfd`-based waiting for external processes. |
| `internal/x11` | Direct X11 communication used to detect the window manager. |
| `data/systemd/user` | Installed systemd user-unit templates. |
| `data/man` | The manual page, written as `uxsm.1.in`. |

## Publishing a release

`make release` builds and tests; `make publish TAG=vX.Y.Z` publishes. They are separate because a release build is also the test gate, run while working, and publishing is a public, one-way act.

`test/publish.sh` does it in this order, stopping at the first thing that does not add up:

1. **Checks.** The version looks like `vX.Y.Z`, the tree is clean, the branch is `main` or `master`, the tag exists neither here nor on the remote, no GitHub release carries that name, and `gh` is logged in.
2. **The tag, locally.** It has to come before the build: `test/version.sh` derives the version from `git describe`, so the packages and the tarballs only carry `X.Y.Z` once the tag exists. If `releases/latest` is not that version, `make release` runs now.
3. **The binary tarball**, next to the packages, and `SHA256SUMS` recomputed with it. Then the tarball is installed into a temporary root and the installed binary is asked its version: a tarball that does not install is not published.
4. **The push and the release.** The commit, the tag, and `gh release create` with the five packages, the source tarball, the binary tarball and `SHA256SUMS`.

Everything up to step 3 stays in the clone, and the tag is undone with `git tag -d`. Step 4 asks first, unless `--yes`, and `DRY_RUN=1` runs nothing at all.

Afterwards, `test/publish.sh --hashes vX.Y.Z` downloads the tag archive GitHub serves and writes its checksum into `packaging/arch/PKGBUILD` and `packaging/nix/package.nix`. It cannot be done earlier: GitHub compresses that archive its own way, so the checksum is only knowable once the tag is there.

## The manual page

`data/man/uxsm.1.in` is the source of `uxsm.1`. `make install` writes it to `$(MANDIR)/man1`, filling in `@VERSION@` and `@BINDIR@`, the same way the unit templates are filled in; the Arch and Debian packages get it from that, and the Fedora, openSUSE and Nix recipes list it themselves.

`make check` lints it with `groff -man -z -ww`. groff exits 0 even when it warns, so the check fails on anything written to stderr, which is what catches an unknown macro or a section that was left open. The integration suite checks the other half: `test/integration/run.sh` fails if the installed package does not ship `man/man1/uxsm.1`, whatever compression the distribution puts on it.
| `test` | Integration scripts, VM orchestration, packages, and distro session data. |
| `packaging` | Native package recipes exercised by release tests. |

## Go version

### Why Go 1.22

The minimum version is Go 1.22 because Ubuntu 24.04 LTS ships it. Requiring a newer version would prevent that distribution from building the package with its native toolchain.

Versions checked on 2026-09-17:

| Distribution                                    | Go version       |
| ----------------------------------------------- | ---------------- |
| Ubuntu 24.04 LTS                                | 1.22             |
| Debian 13 (trixie)                              | 1.24             |
| Ubuntu 26.04 LTS                                | 1.26             |
| Fedora 43                                       | 1.26             |
| Arch Linux, openSUSE Tumbleweed, NixOS unstable | Latest available |

The `.0` in `go.mod` is intentional: `go 1.22` names a language version, not a released Go toolchain. An older Go release attempting automatic toolchain selection needs the full version.

### Why `go vet` runs before every build

A Go version newer than the one declared in `go.mod` can compile calls added to the standard library after Go 1.22. `go vet` catches those compatibility mistakes, so `make build` runs it first.

### APIs unavailable at the minimum version

| Added in | Do not use |
| --- | --- |
| Go 1.23 | Range-over-function iterators; `iter`; `slices.Collect`; `slices.Sorted`; iterator forms of `maps.Keys` and `maps.Values`; `unique`. |
| Go 1.24 | Generic type aliases; `os.Root`; `strings.Lines`; `strings.SplitSeq`; `testing.B.Loop`; JSON `omitzero`. |
| Go 1.25 | `sync.WaitGroup.Go`; `testing/synctest`. |

Go 1.22 features are available, including integer ranges, per-iteration loop variables, `min`, `max`, pre-iterator `slices` and `maps`, `math/rand/v2`, and `log/slog`.
