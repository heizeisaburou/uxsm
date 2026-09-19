#!/bin/sh
# Compila los paquetes de uxsm y los prueba instalados, todo en máquinas
# virtuales desechables (test/vm.sh):
#
#   1. Empaqueta el árbol de trabajo en un tarball, con los cambios sin commitear
#      y los ficheros nuevos que no ignora git.
#   2. Compila el paquete de cada distribución en una máquina de esa
#      distribución, con su herramienta, y lo trae a build/release/<distro>.
#   3. En una máquina limpia de cada distribución, instala su paquete con su
#      gestor de paquetes y ejecuta test/integration/run.sh.
#   4. Con --publish, si todo ha ido bien, build/release pasa a releases/latest.
#
#   test/release.sh [distros]
#   test/release.sh --publish
#
#   distros    quick  ubuntu (por defecto)
#              pair   ubuntu y arch, las dos más distintas
#              all    todas: ubuntu, debian, arch, fedora y opensuse
#              o una lista separada por comas: ubuntu,arch
#
# Cada distribución tiene su propio paquete, aunque Debian y Ubuntu compartan
# formato: cada uno se compila contra las bibliotecas y las herramientas de la
# suya; lo mismo Fedora y openSUSE. --publish compila y prueba en todas: releases/latest es siempre un
# juego completo.

set -eu
cd "$(dirname "$0")/.."

publish=
if [ "${1:-}" = --publish ]; then
    publish=1
    shift
    [ $# -eq 0 ] || { echo "release.sh: --publish always tests every distro" >&2; exit 2; }
    set -- all
fi

case "${1:-quick}" in
quick) distros=ubuntu ;;
pair)  distros="ubuntu arch" ;;
all)   distros="ubuntu debian arch fedora opensuse" ;;
*)     distros=$(printf '%s' "$1" | tr ',' ' ') ;;
esac

# recipe DISTRO: el script de test/package que compila su paquete.
recipe() {
    case "$1" in
    ubuntu | debian)   echo deb ;;
    arch)              echo arch ;;
    fedora | opensuse) echo rpm ;;
    *)                 return 1 ;;
    esac
}

for d in $distros; do
    recipe "$d" >/dev/null || { echo "release.sh: no package for $d yet" >&2; exit 2; }
done

# La versión de los paquetes: test/version.sh.
version=$(test/version.sh)

out=build/release
rm -rf "$out" build/vm
mkdir -p "$out" build/vm
echo "$version" >"$out/version"

echo "== uxsm $version: building and testing on $distros" >&2

# El tarball: los ficheros que git conoce o que no ignora, sin los borrados.
tarball=uxsm-$version.tar.gz
git ls-files --cached --others --exclude-standard | while IFS= read -r f; do
    if [ -e "$f" ]; then printf '%s\n' "$f"; fi
done | LC_ALL=C sort |
    tar --create --file=- --files-from=- --transform="s|^|uxsm-$version/|" \
        --owner=0 --group=0 --numeric-owner |
    gzip -n >"$out/$tarball"

for d in $distros; do
    stage=build/vm/build-$d
    mkdir -p "$stage"
    cp "$out/version" "$out/$tarball" "$stage/"
    cp "test/package/$(recipe "$d").sh" "$stage/package.sh"
    UXSM_VM_UPLOAD=$stage UXSM_VM_DOWNLOAD=$out/$d \
        test/vm.sh "$d" 'sh ~/uxsm/package.sh'
done

# La máquina de cada distribución instala lo que hay en packages/<distro>.
stage=build/vm/test
mkdir -p "$stage/packages"
cp -r test/integration "$stage/integration"
cp "$out/version" "$stage/"
for d in $distros; do
    cp -r "$out/$d" "$stage/packages/$d"
done
UXSM_VM_UPLOAD=$stage test/vm.sh "$(echo $distros | tr ' ' ',')" 'sh ~/uxsm/integration/run.sh'

(cd "$out" && find . -type f ! -name version ! -name SHA256SUMS -printf '%P\n' | LC_ALL=C sort | xargs sha256sum >SHA256SUMS)

if [ -n "$publish" ]; then
    rm -rf releases/latest.new
    mkdir -p releases
    cp -r "$out" releases/latest.new
    rm -rf releases/latest
    mv releases/latest.new releases/latest
    echo "== releases/latest is uxsm $version" >&2
fi
