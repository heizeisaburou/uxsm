#!/bin/sh
# Build the Arch package in a disposable Arch VM using packaging/arch's PKGBUILD
# and makepkg, as in the AUR. test/release.sh runs this after uploading
# ~/uxsm/version and ~/uxsm/uxsm-<version>.tar.gz; the package is left in
# the ~/uxsm/out directory. With `deps`, it only installs the build
# dependencies.
#
# The PKGBUILD downloads the GitHub tag tarball. makepkg downloads nothing when
# a file with that name already sits beside the PKGBUILD, so setting the version
# and placing the tarball there is sufficient.
#
# Its sha256sums is the checksum of the published tarball, which is what the AUR
# needs, and never matches the one built here from the working tree. So it is
# replaced with SKIP: what this tests is that the recipe builds, not that a file
# made three lines above is the file it says it is.

set -eu
cd "$HOME/uxsm"
v=$(cat version)
. "$HOME/uxsm/mirror.sh"

echo "== updating the system"
# An old image may contain a keyring that no longer signs current packages.
mirror sudo pacman -Sy --noconfirm --needed archlinux-keyring >/dev/null
mirror sudo pacman -Su --noconfirm >/dev/null
mirror sudo pacman -S --noconfirm --needed base-devel >/dev/null
if [ "${1:-}" = deps ]; then
  # What makepkg -s would install, so that test/vm.sh keeps it as the build
  # image and makepkg finds it there.
  mkdir build
  cd build
  tar -xzf "../uxsm-$v.tar.gz" --strip-components=3 "uxsm-$v/packaging/arch/PKGBUILD"
  sed -i "s/^pkgver=.*/pkgver=$v/" PKGBUILD
  # shellcheck disable=SC2046
  mirror sudo pacman -S --noconfirm --needed $(makepkg --printsrcinfo |
    sed -n 's/^\t\(make\)\{0,1\}depends = //p') >/dev/null
  exit 0
fi

echo "== building uxsm $v"
mkdir build
cd build
tar -xzf "../uxsm-$v.tar.gz" --strip-components=3 "uxsm-$v/packaging/arch/PKGBUILD"
sed -i "s/^pkgver=.*/pkgver=$v/;s/^sha256sums=.*/sha256sums=('SKIP')/" PKGBUILD
cp "../uxsm-$v.tar.gz" .
# -s installs PKGBUILD makedepends, so a missing dependency fails here.
makepkg -s --noconfirm

mkdir -p "$HOME/uxsm/out"
mv ./*.pkg.tar.zst "$HOME/uxsm/out/"
ls "$HOME/uxsm/out"
