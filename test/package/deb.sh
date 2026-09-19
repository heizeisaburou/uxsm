#!/bin/sh
# Compila el paquete .deb de la distribución de la máquina, Debian o Ubuntu,
# con packaging/debian y dpkg-buildpackage. Lo ejecuta test/release.sh con
# ~/uxsm/version y ~/uxsm/uxsm-<versión>.tar.gz ya subidos, y deja el paquete en
# ~/uxsm/out.

set -eu
cd "$HOME/uxsm"
v=$(cat version)
. /etc/os-release

apt() {
    sudo DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1 apt-get -y -qq "$@" >/dev/null
}

mkdir build
cd build
cp "../uxsm-$v.tar.gz" "uxsm_$v.orig.tar.gz"
tar -xzf "uxsm_$v.orig.tar.gz"
cd "uxsm-$v"
cp -r packaging/debian debian
# La primera línea del changelog es la que da la versión al paquete. La
# revisión lleva la distribución, -1~ubuntu24.04 o -1~debian13: los paquetes de
# cada una no se llaman igual, y con ~ el -1 de un paquete oficial va después.
sed -i "1s/([^)]*)/($v-1~$ID$VERSION_ID)/" debian/changelog

echo "== installing build dependencies"
apt update
apt install --no-install-recommends build-essential dpkg-dev
# Las de Build-Depends en debian/control: si falta alguna, falla aquí.
apt build-dep ./

echo "== building uxsm $v"
dpkg-buildpackage -us -uc -b

mkdir -p "$HOME/uxsm/out"
mv ../*.deb "$HOME/uxsm/out/"
ls "$HOME/uxsm/out"
