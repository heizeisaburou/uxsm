#!/bin/sh
# Punto de entrada de las pruebas de integración. Se ejecuta dentro de una
# máquina desechable, como un usuario con sudo sin contraseña y una sesión de
# logind (la del ssh). test/release.sh sube a ~/uxsm los paquetes de uxsm de
# cada distribución en packages/<distro> y la versión que deben dar en version.
#
# Instala el paquete de uxsm con el gestor de paquetes, como lo instalaría
# cualquiera, más las dependencias de las pruebas, y ejecuta cada NN-*.sh por
# orden. Termina con error si falla alguna.

set -eu
here=$(cd "$(dirname "$0")" && pwd)

# Los de esta distribución, en packages/<distro>; vm.sh dice cuál es. Los globs
# toman sólo el paquete principal, no los de depuración (uxsm-dbgsym,
# uxsm-debug, uxsm-debuginfo, uxsm-debugsource), que también se compilan.
packages=$HOME/uxsm/packages/$UXSM_VM_DISTRO

if command -v apt-get >/dev/null 2>&1; then
    apt() { sudo DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1 apt-get -y -qq "$@" >/dev/null; }
    echo "== installing test dependencies"
    apt update
    apt install xvfb x11-utils bspwm
    echo "== installing uxsm"
    # apt, no dpkg -i: resuelve las Depends del paquete, y falla si no puede.
    apt install "$packages"/uxsm_*.deb
    files=$(dpkg -L uxsm)
elif command -v pacman >/dev/null 2>&1; then
    echo "== updating the system"
    # El paquete se compiló contra un sistema al día; éste tiene que estarlo.
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
    # Un paquete compilado aquí no está firmado.
    zypper install --allow-unsigned-rpm "$packages"/uxsm-[0-9]*.rpm
    files=$(rpm -ql uxsm)
else
    echo "run.sh: no supported package manager" >&2
    exit 1
fi

for f in $files; do
    [ -d "$f" ] || echo "  $f"
done

# Lo que el paquete haya traído para la sesión, como el bus de D-Bus con
# dbus-user-session, lo arrancaría el siguiente inicio de sesión; éste empezó
# antes de instalar. Sólo si hace falta: openSUSE no deja arrancarlo a mano
# (RefuseManualStart=), aunque allí ya está en marcha.
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
