#!/bin/sh
# Compila el paquete de Arch dentro de una máquina de Arch desechable, con el
# PKGBUILD de packaging/arch y makepkg, como en el AUR. Lo ejecuta
# test/release.sh con ~/uxsm/version y ~/uxsm/uxsm-<versión>.tar.gz ya subidos, y
# deja el paquete en ~/uxsm/out.
#
# El PKGBUILD descarga el tarball de la etiqueta de GitHub; makepkg no descarga
# nada si ya hay un fichero con ese nombre junto al PKGBUILD, así que basta con
# ponerle la versión y dejar ahí el tarball.

set -eu
cd "$HOME/uxsm"
v=$(cat version)

echo "== updating the system"
# Una imagen vieja puede traer un llavero que ya no firma los paquetes nuevos.
sudo pacman -Sy --noconfirm --needed archlinux-keyring >/dev/null
sudo pacman -Su --noconfirm >/dev/null
sudo pacman -S --noconfirm --needed base-devel >/dev/null

echo "== building uxsm $v"
mkdir build
cd build
tar -xzf "../uxsm-$v.tar.gz" --strip-components=3 "uxsm-$v/packaging/arch/PKGBUILD"
sed -i "s/^pkgver=.*/pkgver=$v/" PKGBUILD
cp "../uxsm-$v.tar.gz" .
# -s instala los makedepends del PKGBUILD: si falta alguno, falla aquí.
makepkg -s --noconfirm

mkdir -p "$HOME/uxsm/out"
mv ./*.pkg.tar.zst "$HOME/uxsm/out/"
ls "$HOME/uxsm/out"
