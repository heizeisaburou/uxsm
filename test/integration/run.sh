#!/bin/sh
# Integration-test entry point. It runs inside a disposable VM as a user with
# passwordless sudo and a logind session (the SSH session). test/release.sh
# uploads each distribution's uxsm packages under packages/<distro> and the
# expected output of the version command to the ~/uxsm directory.
#
# Install the uxsm package through the distribution package manager, as a user
# would, plus test dependencies, then run each NN-*.sh in order. Exit with an
# error if any test fails.

set -eu
here=$(cd "$(dirname "$0")" && pwd)

# Packages for this distribution live in packages/<distro>; vm.sh identifies it.
# Globs select only the main package, not debug packages (uxsm-dbgsym, uxsm-debug,
# uxsm-debuginfo, uxsm-debugsource), which are built too.
packages=$HOME/uxsm/packages/$UXSM_VM_DISTRO

if command -v apt-get >/dev/null 2>&1; then
    apt() { sudo DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1 apt-get -y -qq "$@" >/dev/null; }
    echo "== installing test dependencies"
    apt update
    apt install xvfb x11-utils bspwm
    echo "== installing uxsm"
    # Use apt rather than dpkg -i: it resolves package Depends and fails if it cannot.
    apt install "$packages"/uxsm_*.deb
    files=$(dpkg -L uxsm)
elif command -v pacman >/dev/null 2>&1; then
    echo "== updating the system"
    # The package was built against an updated system, so this one must match.
    sudo pacman -Sy --noconfirm --needed archlinux-keyring >/dev/null
    sudo pacman -Su --noconfirm >/dev/null
    echo "== installing test dependencies"
    sudo pacman -S --noconfirm --needed xorg-server-xvfb xorg-xprop bspwm >/dev/null
    echo "== installing uxsm"
    sudo pacman -U --noconfirm "$packages"/uxsm-[0-9]*.pkg.tar.zst >/dev/null
    files=$(pacman -Qlq uxsm)
elif command -v dnf >/dev/null 2>&1; then
    echo "== installing test dependencies"
    sudo dnf -y -q install xorg-x11-server-Xvfb xprop bspwm >/dev/null
    echo "== installing uxsm"
    sudo dnf -y -q install "$packages"/uxsm-[0-9]*.rpm >/dev/null
    files=$(rpm -ql uxsm)
elif command -v zypper >/dev/null 2>&1; then
    zypper() { sudo zypper --non-interactive --quiet "$@" >/dev/null; }
    zypper refresh
    echo "== installing test dependencies"
    zypper install --no-recommends xorg-x11-server-Xvfb xprop bspwm
    echo "== installing uxsm"
    # A package built here is unsigned.
    zypper install --allow-unsigned-rpm "$packages"/uxsm-[0-9]*.rpm
    files=$(rpm -ql uxsm)
else
    echo "run.sh: no supported package manager" >&2
    exit 1
fi

for f in $files; do
    [ -d "$f" ] || echo "  $f"
done

# The package must contain the manual, compressed according to the distribution.
echo "$files" | grep -q "man/man1/uxsm\.1" || {
    echo "run.sh: the package does not ship the man page" >&2
    exit 1
}

# Session components installed by the package, such as the D-Bus bus from
# dbus-user-session, would start at the next login; this login predates package
# installation. Start them only when needed: openSUSE forbids manual startup
# (RefuseManualStart=), though the bus is already running there.
systemctl --user daemon-reload
systemctl --user is-active -q dbus.socket || systemctl --user start dbus.socket
got=$(uxsm version)
want=$(cat "$HOME/uxsm/version")
[ "$got" = "$want" ] || { echo "run.sh: uxsm version is $got, expected $want" >&2; exit 1; }
echo "uxsm $got"

failed=0
for t in "$here"/[0-9][0-9]-*.sh; do
    echo "== $(basename "$t")"
    sh "$t" || failed=$((failed + 1))
done

if [ "$failed" -gt 0 ]; then
    echo "== $failed test(s) failed"
    exit 1
fi
echo "== all tests passed"
