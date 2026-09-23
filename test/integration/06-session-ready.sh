#!/bin/sh
# Paso 4: la sesión no está lista hasta que lo dice una de dos cosas, y sólo
# cuenta la primera: uxsm ve el gestor de ventanas EWMH en la pantalla, o el
# propio escritorio ejecuta `uxsm finalize`. Hasta entonces el escritorio no
# cuenta como arrancado, así que graphical-session.target espera con él; y si no
# llega ninguna de las dos, la sesión no se queda a medias: el servicio falla al
# agotarse TimeoutStartSec= y todo se apaga.

set -eu
. "$(dirname "$0")/lib.sh"

entries=$HOME/.local/share/xsessions
slow=$HOME/uxsm-it-slowwm
final=$HOME/uxsm-it-finalize
dropin=$HOME/.config/systemd/user/uxsm-desktop@.service.d
slow_unit=uxsm-desktop@uxsm-it-slowwm.desktop.service
final_unit=uxsm-desktop@uxsm-it-finalize.desktop.service
nowm=uxsm-desktop@sleep.service

# Las llamadas sueltas a `uxsm aux wait-ready` de aquí abajo no son una sesión:
# nadie apaga detrás la señal de que la sesión está lista, que dentro de una
# sesión apagan `uxsm start` al empezar y la limpieza al cerrar. Como una señal
# encendida es justo lo que la espera busca, hay que apagarla entre llamada y
# llamada para que cada una empiece de cero.
clear_ready() { rm -f "$XDG_RUNTIME_DIR/uxsm/ready"; }

cleanup() {
    systemctl --user stop uxsm-it-session.service "$slow_unit" "$final_unit" "$nowm" \
        uxsm-it-wm.service uxsm-it-xvfb-5.service 2>/dev/null || true
    clear_ready
    rm -f "$entries/uxsm-it-slowwm.desktop" "$entries/uxsm-it-finalize.desktop" \
        "$slow" "$final" "$dropin/uxsm-it-timeout.conf"
    rmdir "$dropin" 2>/dev/null || true
    systemctl --user daemon-reload
}
trap cleanup EXIT

# Las pruebas de antes pueden estar todavía cerrando su última sesión.
wait_no_session || fail "a session from an earlier test is still shutting down"

# Sin sesión, no hay nada que dar por listo.
if err=$(uxsm finalize 2>&1); then
    fail "uxsm finalize outside a session did not fail"
fi
case $err in
*"no uxsm session"*) ok "uxsm finalize outside a session says there is none" ;;
*) fail "uxsm finalize outside a session: $err" ;;
esac

start_xvfb :5

# Sin gestor de ventanas y sin aviso del escritorio, la espera se agota y lo dice.
clear_ready
if err=$(DISPLAY=:5 uxsm aux wait-ready -timeout 2s 2>&1); then
    fail "uxsm aux wait-ready succeeded on a display with no window manager"
fi
case $err in
*"no EWMH window manager"*) ok "with neither of the two, the wait times out and says so" ;;
*) fail "uxsm aux wait-ready on a bare display: $err" ;;
esac

# Con gestor de ventanas, dice cuál es.
clear_ready
systemd-run --user --quiet --collect --unit=uxsm-it-wm -E DISPLAY=:5 bspwm
got=$(DISPLAY=:5 uxsm aux wait-ready -timeout 10s)
case $got in
*"window manager ready: bspwm"*) ok "with a window manager, the wait reports its name" ;;
*) fail "uxsm aux wait-ready with bspwm printed: $got" ;;
esac

# Y cuando el gestor de ventanas se va, lo que pueda quedar de él en la raíz no
# cuenta: la ventana a la que apunta la marca ya no existe.
systemctl --user stop uxsm-it-wm.service
clear_ready
if err=$(DISPLAY=:5 uxsm aux wait-ready -timeout 2s 2>&1); then
    fail "uxsm aux wait-ready succeeded after the window manager was gone"
fi
ok "the mark of a window manager that is gone does not count"

# Un escritorio que tarda en poner su gestor de ventanas: si la sesión no
# esperara, graphical-session.target estaría activo estos dos segundos antes de
# que hubiera dónde colocar nada.
cat >"$slow" <<'DESKTOP'
#!/bin/sh
sleep 2
exec bspwm
DESKTOP
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
wait_stopped 15 uxsm-it-session.service "$slow_unit" || fail "the session did not stop"

# El otro camino: un escritorio sin gestor de ventanas EWMH que lo dice él.
cat >"$final" <<'DESKTOP'
#!/bin/sh
uxsm finalize
exec sleep infinity
DESKTOP
chmod 755 "$final"
cat >"$entries/uxsm-it-finalize.desktop" <<ENTRY
[Desktop Entry]
Type=Application
Name=uxsm test, finalize
Exec=$final
ENTRY

systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 \
    uxsm start uxsm-it-finalize.desktop
wait_for 20 systemctl --user is-active graphical-session.target ||
    fail "the session of a desktop that runs uxsm finalize did not become ready"
ok "a desktop with no window manager becomes ready by running uxsm finalize"

out=$(uxsm finalize) || fail "a second uxsm finalize failed: $out"
case $out in
*"already ready"*) ok "and the signal is only turned on once" ;;
*) fail "a second uxsm finalize said: $out" ;;
esac

uxsm stop
wait_stopped 15 uxsm-it-session.service "$final_unit" || fail "the finalized session did not stop"

# Ni gestor de ventanas ni aviso: la sesión no se queda a medias. El tiempo de
# espera de la unidad se acorta con un fichero de anulación, que es además la
# forma de quitar la espera a quien arranque algo que no es un escritorio.
mkdir -p "$dropin"
cat >"$dropin/uxsm-it-timeout.conf" <<CONF
[Service]
TimeoutStartSec=5
CONF
systemctl --user daemon-reload

systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 uxsm start -- sleep infinity
sleep 2
if systemctl --user is-active -q graphical-session.target; then
    fail "the session became ready with neither a window manager nor uxsm finalize"
fi
ok "with neither of the two, the session never becomes ready"

wait_stopped 20 uxsm-it-session.service "$nowm" uxsm-env@sleep.service graphical-session.target ||
    fail "the session that was never ready did not stop when the wait timed out"
ok "and the whole session is shut down when the wait times out"
