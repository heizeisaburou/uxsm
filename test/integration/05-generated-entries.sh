#!/bin/sh
# uxsm entry: las entradas que genera arrancan la sesión, con los nombres del
# escritorio bien puestos, y no pisa ni tapa otras entradas sin -f. Y uxsm
# check y uxsm setup xsessions-dir en una máquina sin display manager.
#
# El display manager se sustituye, como en las demás pruebas, por una unidad
# pasajera que ejecuta el Exec= de la entrada generada. Lo hace sin
# XDG_CURRENT_DESKTOP, como un display manager que no pasara el DesktopNames=
# de la entrada: el peor caso, en el que los nombres sólo llegan por el Exec=.

set -eu
. "$(dirname "$0")/lib.sh"

dir=/usr/local/share/xsessions

cleanup() {
    systemctl --user stop uxsm-it-session.service uxsm-desktop@bspwm.desktop.service \
        uxsm-desktop@bspwm.service uxsm-it-xvfb-5.service 2>/dev/null || true
    sudo rm -f "$dir/bspwm-uxsm.desktop" "$dir/bspwm.desktop"
}
trap cleanup EXIT

# run_entry ENTRADA UNIDAD: arranca la sesión con el Exec= de ENTRADA y espera a
# que UNIDAD, la del escritorio, esté activa. Deja en $desktop la unidad.
run_entry() {
    exec_line=$(sed -n 's/^Exec=//p' "$dir/$1")
    systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 \
        env -u XDG_CURRENT_DESKTOP sh -c "exec $exec_line"
    desktop=$2
    wait_for 15 systemctl --user is-active "$desktop" || fail "$1 did not start $desktop"
    wait_for 10 sh -c 'DISPLAY=:5 xprop -root _NET_SUPPORTING_WM_CHECK | grep -q "window id"' ||
        fail "bspwm is not managing :5"
}

# quit_session: sale de bspwm y espera a que no quede nada de la sesión.
quit_session() {
    DISPLAY=:5 bspc quit
    wait_stopped 15 uxsm-it-session.service "$desktop" || fail "the session of $desktop did not stop"
}

out=$(uxsm check) || fail "uxsm check without a display manager failed: $out"
case $out in
*"xsessions dir: unknown: no display manager"*) ok "uxsm check says it cannot tell without a display manager" ;;
*) fail "uxsm check: $out" ;;
esac
if uxsm setup xsessions-dir lightdm >/dev/null 2>&1; then
    fail "uxsm setup xsessions-dir lightdm worked without LightDM installed"
fi
ok "uxsm setup xsessions-dir refuses a display manager that is not installed"

# Sin -i sólo enseña lo que escribiría.
sudo uxsm entry bspwm 2>/dev/null | grep -q "^Would write $dir/bspwm-uxsm.desktop:" ||
    fail "uxsm entry without -i did not show what it would write"
[ ! -e "$dir/bspwm-uxsm.desktop" ] || fail "uxsm entry without -i wrote the entry"
ok "without -i, uxsm entry only shows the entry"

start_xvfb :5

# La que apunta a bspwm.desktop.
sudo uxsm entry -i bspwm >/dev/null 2>&1 || fail "uxsm entry -i bspwm failed"
run_entry bspwm-uxsm.desktop uxsm-desktop@bspwm.desktop.service
expect_var XDG_CURRENT_DESKTOP bspwm
ok "bspwm-uxsm.desktop starts bspwm.desktop as bspwm"
quit_session

# La del comando directo: no pisa la anterior sin -f.
if sudo uxsm entry -i --exec bspwm 2>/dev/null; then
    fail "uxsm entry overwrote bspwm-uxsm.desktop without -f"
fi
ok "uxsm entry does not overwrite an entry without -f"
sudo uxsm entry -i -f --exec bspwm >/dev/null 2>&1 || fail "uxsm entry -i -f --exec bspwm failed"
run_entry bspwm-uxsm.desktop uxsm-desktop@bspwm.service
expect_var XDG_CURRENT_DESKTOP bspwm
ok "bspwm-uxsm.desktop with the command starts bspwm directly, as bspwm"
quit_session

# La normal taparía la del paquete, en /usr/share/xsessions.
if sudo uxsm entry -i --plain bspwm 2>/dev/null; then
    fail "uxsm entry --plain hid the package's bspwm.desktop without -f"
fi
[ ! -e "$dir/bspwm.desktop" ] || fail "uxsm entry --plain wrote bspwm.desktop"
ok "uxsm entry does not hide a package's entry without -f"
