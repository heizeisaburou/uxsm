#!/bin/sh
# Paso 1: `uxsm start` lanza el Exec= de la entrada como servicio de systemd
# --user, y la sesión dura lo que dura el escritorio.
#
# El display manager se sustituye por una unidad pasajera, uxsm-it-session,
# que ejecuta `uxsm start` con DISPLAY puesto, como lo haría LightDM.

set -eu
. "$(dirname "$0")/lib.sh"

unit=uxsm-desktop@bspwm.desktop.service

cleanup() {
    systemctl --user stop uxsm-it-session.service "$unit" uxsm-it-xvfb-5.service 2>/dev/null || true
}
trap cleanup EXIT

if uxsm start missing.desktop 2>/dev/null; then
    fail "uxsm start with a missing entry did not fail"
fi
ok "a missing entry is an error"

start_xvfb :5
systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 uxsm start bspwm.desktop

wait_for 15 systemctl --user is-active "$unit" || fail "$unit is not active"
ok "$unit is active"

main=$(systemctl --user show -p MainPID --value "$unit")
comm=$(cat "/proc/$main/comm")
[ "$comm" = bspwm ] || fail "the main process of $unit is $comm, not bspwm"
ok "its main process is bspwm itself"

systemctl --user show-environment | grep -qx 'DISPLAY=:5' || fail "DISPLAY=:5 is not in the systemd environment"
ok "DISPLAY=:5 reached the systemd environment"

wait_for 10 sh -c 'DISPLAY=:5 xprop -root _NET_SUPPORTING_WM_CHECK | grep -q "window id"' ||
    fail "bspwm is not managing :5"
ok "bspwm manages the display"

DISPLAY=:5 bspc quit
wait_for 10 sh -c '! systemctl --user is-active uxsm-it-session.service' ||
    fail "the session process is still running after bspc quit"
ok "quitting bspwm ends the session process"
