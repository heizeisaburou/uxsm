#!/bin/sh
# Paso 4: la sesión no está lista hasta que hay gestor de ventanas. El
# escritorio no cuenta como arrancado hasta que uxsm ve _NET_SUPPORTING_WM_CHECK
# en la pantalla, así que graphical-session.target espera con él; y un escritorio
# que nunca llega a tener gestor de ventanas no deja una sesión a medias: falla
# al agotarse TimeoutStartSec= y la sesión se apaga entera.

set -eu
. "$(dirname "$0")/lib.sh"

entries=$HOME/.local/share/xsessions
slow=$HOME/uxsm-it-slowwm
dropin=$HOME/.config/systemd/user/uxsm-desktop@.service.d
desktop=uxsm-desktop@uxsm-it-slowwm.desktop.service
nowm=uxsm-desktop@sleep.service

cleanup() {
    systemctl --user stop uxsm-it-session.service "$desktop" "$nowm" uxsm-it-xvfb-5.service 2>/dev/null || true
    rm -f "$entries/uxsm-it-slowwm.desktop" "$slow" "$dropin/uxsm-it-timeout.conf"
    rmdir "$dropin" 2>/dev/null || true
    systemctl --user daemon-reload
}
trap cleanup EXIT

start_xvfb :5

# Sin gestor de ventanas, la espera se agota y lo dice.
if err=$(DISPLAY=:5 uxsm aux wait-wm -timeout 2s 2>&1); then
    fail "uxsm aux wait-wm succeeded on a display with no window manager"
fi
case $err in
*"no EWMH window manager"*) ok "without a window manager, the wait times out and says so" ;;
*) fail "uxsm aux wait-wm on a bare display: $err" ;;
esac

# Con uno, dice cuál es.
systemd-run --user --quiet --collect --unit=uxsm-it-wm -E DISPLAY=:5 bspwm
got=$(DISPLAY=:5 uxsm aux wait-wm -timeout 10s)
case $got in
*"window manager ready: bspwm"*) ok "with a window manager, the wait reports its name" ;;
*) fail "uxsm aux wait-wm with bspwm printed: $got" ;;
esac
# Y cuando el gestor de ventanas se va, lo que pueda quedar de él en la raíz no
# cuenta: la ventana a la que apunta la marca ya no existe.
systemctl --user stop uxsm-it-wm.service
if err=$(DISPLAY=:5 uxsm aux wait-wm -timeout 2s 2>&1); then
    fail "uxsm aux wait-wm succeeded after the window manager was gone"
fi
ok "the mark of a window manager that is gone does not count"

# Un escritorio que tarda en poner su gestor de ventanas: si la sesión no
# esperara, graphical-session.target estaría activo estos dos segundos antes de
# que hubiera dónde colocar nada.
cat >"$slow" <<'ENTRY'
#!/bin/sh
sleep 2
exec bspwm
ENTRY
chmod 755 "$slow"
mkdir -p "$entries"
cat >"$entries/uxsm-it-slowwm.desktop" <<ENTRY
[Desktop Entry]
Type=Application
Name=uxsm test, slow window manager
Exec=$slow
ENTRY

began=$(date +%s)
systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 \
    uxsm start uxsm-it-slowwm.desktop
wait_for 30 systemctl --user is-active graphical-session.target ||
    fail "the session of the slow desktop did not start"
elapsed=$(( $(date +%s) - began ))
[ "$elapsed" -ge 2 ] ||
    fail "the session was ready in ${elapsed}s, before its window manager was up"
DISPLAY=:5 xprop -root _NET_SUPPORTING_WM_CHECK 2>/dev/null | grep -q "window id" ||
    fail "graphical-session.target was reached before the window manager was up"
ok "graphical-session.target waits for the window manager"

DISPLAY=:5 bspc quit
wait_stopped 15 uxsm-it-session.service "$desktop" || fail "the session did not stop"

# Un escritorio que nunca tiene gestor de ventanas. El tiempo de espera de la
# unidad se acorta con un fichero de anulación, que es además la forma de
# quitar la espera a quien arranque algo que no es un escritorio.
mkdir -p "$dropin"
cat >"$dropin/uxsm-it-timeout.conf" <<CONF
[Service]
TimeoutStartSec=5
CONF
systemctl --user daemon-reload

systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 uxsm start -- sleep infinity
sleep 2
if systemctl --user is-active -q graphical-session.target; then
    fail "the session became ready without a window manager"
fi
ok "without a window manager the session never becomes ready"

wait_stopped 20 uxsm-it-session.service "$nowm" uxsm-env@sleep.service graphical-session.target ||
    fail "the session without a window manager did not stop when the wait timed out"
ok "and the whole session is shut down when the wait times out"
