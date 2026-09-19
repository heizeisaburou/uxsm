#!/bin/sh
# Recoge las entradas de sesión X11 (/usr/share/xsessions) que traen los
# paquetes de cada distribución, sin instalarlos, con test/xsessions/collect.sh
# en máquinas de test/vm.sh. Son los datos de los que sale la tabla de
# escritorios conocidos de internal/sessionentry.
#
#   test/xsessions.sh [distros]     por defecto: arch,debian,ubuntu,fedora
#
# Deja cada una en build/xsessions/<distro>: las entradas y su index.
# openSUSE no está: zypper no busca paquetes por rutas con comodines.

set -eu
cd "$(dirname "$0")/.."

distros=${1:-arch,debian,ubuntu,fedora}
for d in $(echo "$distros" | tr ',' ' '); do
    rm -rf "build/xsessions/$d"
    UXSM_VM_UPLOAD=test/xsessions UXSM_VM_DOWNLOAD=build/xsessions/$d \
        test/vm.sh "$d" 'sh ~/uxsm/collect.sh'
done
