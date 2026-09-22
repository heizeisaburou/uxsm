#!/bin/sh
# Paso 5: el autostart XDG. Con un gestor de ventanas, uxsm activa
# xdg-desktop-autostart.target y las entradas de ~/.config/autostart arrancan
# como app-<nombre>@autostart.service, una sola vez, y se paran con la sesión.
# Con un escritorio que lanza el suyo, uxsm no lo lanza: arrancarían dos veces.
# `uxsm start -a yes` fuerza que lo lance de todos modos.

set -eu
. "$(dirname "$0")/lib.sh"

probe=$HOME/uxsm-it-probe
log=$HOME/uxsm-it-autostart.log
entry=$HOME/.config/autostart/uxsm-it-probe.desktop
unit="app-$(systemd-escape uxsm-it-probe)@autostart.service"
target=uxsm-autostart@bspwm.desktop.target

# diagnose enseña el estado de las unidades del autostart cuando algo no sale,
# que si no hay que ir a buscarlo a la máquina.
diagnose() {
    systemctl --user list-units --all --no-legend "$target" xdg-desktop-autostart.target "$unit" || true
    journalctl --user -n 15 --no-pager -u "$target" -u uxsm-desktop@bspwm.desktop.service || true
}

cleanup() {
    uxsm stop 2>/dev/null || true
    systemctl --user stop uxsm-it-session.service uxsm-it-xvfb-5.service 2>/dev/null || true
    rm -f "$probe" "$entry" "$log"
    systemctl --user daemon-reload
}
trap cleanup EXIT

# La sonda apunta cada vez que la ejecutan y se queda viva, como un applet.
cat >"$probe" <<'PROBE'
#!/bin/sh
printf '%s %s\n' "$(date +%T)" "${XDG_CURRENT_DESKTOP:-}" >>"$HOME/uxsm-it-autostart.log"
exec sleep infinity
PROBE
chmod 755 "$probe"
mkdir -p "$(dirname "$entry")"
cat >"$entry" <<ENTRY
[Desktop Entry]
Type=Application
Name=uxsm integration test probe
Exec=$probe
ENTRY
# El generador de systemd sólo crea la unidad si el ejecutable existe, así que
# la sonda tiene que estar escrita antes de esto.
systemctl --user daemon-reload

runs() { [ -f "$log" ] && wc -l <"$log" || echo 0; }

start_xvfb :5

# Un gestor de ventanas conocido: el autostart es cosa de uxsm.
run_session bspwm.desktop --
wait_for 15 systemctl --user is-active "$unit" || {
    diagnose
    fail "the autostart entry did not start with the session"
}
ok "with a window manager, uxsm starts the XDG autostart"

# La unidad está activa en cuanto systemd ejecuta la entrada, antes de que ésta
# apunte nada, así que hay que esperar a su rastro; y después un momento más,
# que es donde se vería una segunda vez.
wait_for 15 test -s "$log" || fail "the autostart entry did not run"
sleep 2
[ "$(runs)" = 1 ] || fail "the autostart entry ran $(runs) times, expected once"
ok "and its entry runs exactly once"

grep -q bspwm "$log" || fail "the autostart entry did not get XDG_CURRENT_DESKTOP=bspwm: $(cat "$log")"
ok "with XDG_CURRENT_DESKTOP, which is what filters OnlyShowIn="

stop_session
wait_stopped 15 "$unit" xdg-desktop-autostart.target || fail "the autostart entry outlived the session"
ok "and it stops with the session"

# Un escritorio que lanza el suyo: uxsm no lo lanza. Aquí el escritorio sigue
# siendo bspwm, pero la sesión se llama XFCE, que es lo que uxsm mira.
rm -f "$log"
run_session bspwm.desktop -- -D XFCE
sleep 3
if systemctl --user is-active -q "$unit"; then
    diagnose
    fail "uxsm started the XDG autostart of a desktop that starts its own"
fi
[ "$(runs)" = 0 ] || fail "the autostart entry ran $(runs) times in an XFCE session"
ok "for a desktop that starts its own autostart, uxsm does not start it"
stop_session

# Y se puede forzar.
run_session bspwm.desktop -- -a yes -D XFCE
wait_for 15 systemctl --user is-active "$unit" || fail "uxsm start -a yes did not start the XDG autostart"
ok "uxsm start -a yes starts it anyway"
stop_session

if uxsm start -a maybe bspwm.desktop 2>/dev/null; then
    fail "uxsm start -a maybe did not fail"
fi
ok "a bad -a is a usage error"
