#!/bin/sh
# Punto de entrada de las pruebas de integración. Se ejecuta dentro de una
# máquina desechable, como un usuario con sudo sin contraseña y una sesión de
# logind (la del ssh), con uxsm ya compilado en ~/uxsm/root.
#
# Instala las dependencias de las pruebas y uxsm, y ejecuta cada NN-*.sh por
# orden. Termina con error si falla alguna.

set -eu
here=$(cd "$(dirname "$0")" && pwd)

install_deps() {
    if command -v apt-get >/dev/null 2>&1; then
        sudo DEBIAN_FRONTEND=noninteractive apt-get update -qq
        sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq xvfb x11-utils bspwm >/dev/null
    elif command -v pacman >/dev/null 2>&1; then
        sudo pacman -Sy --noconfirm --needed xorg-server-xvfb xorg-xprop bspwm >/dev/null
    else
        echo "run.sh: no supported package manager" >&2
        exit 1
    fi
}

echo "== installing test dependencies"
install_deps

echo "== installing uxsm"
sudo cp -r "$HOME/uxsm/root/." /
systemctl --user daemon-reload
uxsm version

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
