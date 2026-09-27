#!/bin/sh
# Collect X11 session entries (/usr/share/xsessions) shipped by each
# distribution's packages without installing them, using test/xsessions/collect.sh
# in test/vm.sh machines. These are the source data for the known-desktop table
# in the internal/sessionentry package.
#
#   test/xsessions.sh [distros]     default: arch,debian,ubuntu,fedora
#
# Leave each result under build/xsessions/<distro> with its entries and index.
# openSUSE is omitted because zypper cannot search packages by wildcard paths.

set -eu
cd "$(dirname "$0")/.."

distros=${1:-arch,debian,ubuntu,fedora}
for d in $(echo "$distros" | tr ',' ' '); do
    rm -rf "build/xsessions/$d"
    UXSM_VM_UPLOAD=test/xsessions UXSM_VM_DOWNLOAD=build/xsessions/$d \
        test/vm.sh "$d" 'sh ~/uxsm/collect.sh'
done
