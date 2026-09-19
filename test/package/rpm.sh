#!/bin/sh
# Compila el paquete .rpm de la distribución de la máquina, Fedora u openSUSE,
# con su .spec de packaging/ y rpmbuild. Lo ejecuta test/release.sh con
# ~/uxsm/version y ~/uxsm/uxsm-<versión>.tar.gz ya subidos, y deja los paquetes
# en ~/uxsm/out.
#
# El .spec descarga el tarball de la etiqueta de GitHub; rpmbuild lo busca en
# SOURCES por el nombre del final de Source0, así que basta con ponerle la
# versión y dejar ahí el tarball.

set -eu
cd "$HOME/uxsm"
v=$(cat version)

if command -v dnf >/dev/null 2>&1; then
    pm_install() { sudo dnf -y -q install "$@" >/dev/null; }
else
    pm_install() {
        sudo zypper --non-interactive --quiet install --no-recommends "$@" >/dev/null
    }
    sudo zypper --non-interactive --quiet refresh >/dev/null
fi

mkdir -p "$HOME/rpmbuild/SOURCES" "$HOME/rpmbuild/SPECS"
cp "uxsm-$v.tar.gz" "$HOME/rpmbuild/SOURCES/"
spec=$HOME/rpmbuild/SPECS/uxsm.spec
tar -xzOf "uxsm-$v.tar.gz" "uxsm-$v/packaging/$UXSM_VM_DISTRO/uxsm.spec" >"$spec"
sed -i "s/^Version:\( *\).*/Version:\1$v/" "$spec"

echo "== installing build dependencies"
pm_install rpm-build
# Las de BuildRequires en el .spec: si falta alguna, falla aquí.
buildrequires=$(rpmspec -q --buildrequires "$spec")
set --
while IFS= read -r dep; do
    set -- "$@" "$dep"
done <<EOF
$buildrequires
EOF
pm_install "$@"

echo "== building uxsm $v"
rpmbuild -bb "$spec"

mkdir -p "$HOME/uxsm/out"
mv "$HOME"/rpmbuild/RPMS/*/*.rpm "$HOME/uxsm/out/"
ls "$HOME/uxsm/out"
