#!/bin/sh
# Build the Arch package in a disposable Arch VM using packaging/arch's PKGBUILD
# and makepkg, as in the AUR. test/release.sh runs this after uploading
# ~/uxsm/version and ~/uxsm/uxsm-<version>.tar.gz; the package is left in
# the ~/uxsm/out directory.
#
# The PKGBUILD downloads the GitHub tag tarball. makepkg downloads nothing when
# a file with that name already sits beside the PKGBUILD, so setting the version
# and placing the tarball there is sufficient.

set -eu
cd "$HOME/uxsm"
v=$(cat version)

echo "== updating the system"
# An old image may contain a keyring that no longer signs current packages.
sudo pacman -Sy --noconfirm --needed archlinux-keyring >/dev/null
sudo pacman -Su --noconfirm >/dev/null
sudo pacman -S --noconfirm --needed base-devel >/dev/null

echo "== building uxsm $v"
mkdir build
cd build
tar -xzf "../uxsm-$v.tar.gz" --strip-components=3 "uxsm-$v/packaging/arch/PKGBUILD"
sed -i "s/^pkgver=.*/pkgver=$v/" PKGBUILD
cp "../uxsm-$v.tar.gz" .
# -s installs PKGBUILD makedepends, so a missing dependency fails here.
makepkg -s --noconfirm

mkdir -p "$HOME/uxsm/out"
mv ./*.pkg.tar.zst "$HOME/uxsm/out/"
ls "$HOME/uxsm/out"
