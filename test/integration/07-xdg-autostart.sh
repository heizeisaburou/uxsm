#!/bin/sh
# Paso 5: el autostart XDG. uxsm activa xdg-desktop-autostart.target, y las
# entradas de ~/.config/autostart arrancan como app-<nombre>@autostart.service,
# una sola vez, y se paran con la sesión. Lo hace siempre, como uwsm, salvo que
# se le diga que no con `uxsm start --no-autostart`, que es lo que llevan las
# entradas que genera uxsm para los escritorios que lanzan el suyo.

set -eu
. "$(dirname "$0")/lib.sh"

probe=$HOME/uxsm-it-probe
log=$HOME/uxsm-it-autostart.log
entry=$HOME/.config/autostart/uxsm-it-probe.desktop
unit="app-$(systemd-escape uxsm-it-probe)@autostart.service"
target=uxsm-autostart@bspwm.desktop.target
dropin=$XDG_RUNTIME_DIR/systemd/user/app-@autostart.service.d/uxsm-tweaks.conf

# diagnose enseña el estado de las unidades del autostart cuando algo no sale,
# que si no hay que ir a buscarlo a la máquina.
diagnose() {
    systemctl --user list-units --all --no-legend "$target" xdg-desktop-autostart.target "$unit" || true
    journalctl --user -n 15 --no-pager -u "$target" -u uxsm-desktop@bspwm.desktop.service || true
}

cleanup() {
    uxsm stop 2>/dev/null || true
    systemctl --user stop uxsm-it-session.service uxsm-it-xvfb-5.service 2>/dev/null || true
    rm -f "$probe" "$entry" "$log" "$dropin"
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

# Y cae en el slice de uxsm, no en el app.slice donde la deja el generador: eso
# lo hace el añadido que uxsm escribe al arrancar el autostart.
slice=$(systemctl --user show -p Slice --value "$unit")
[ "$slice" = app-uxsm.slice ] || fail "the autostart entry is in $slice, not app-uxsm.slice"
ok "in the applications slice of the session, not in app.slice"

grep -q bspwm "$log" || fail "the autostart entry did not get XDG_CURRENT_DESKTOP=bspwm: $(cat "$log")"
ok "with XDG_CURRENT_DESKTOP, which is what filters OnlyShowIn="

stop_session
wait_stopped 15 "$unit" xdg-desktop-autostart.target || fail "the autostart entry outlived the session"
ok "and it stops with the session"

# El añadido es de la sesión, no del usuario: al cerrarla no queda nada que
# cambie el slice de las sesiones que vengan detrás, las de uwsm entre ellas.
[ ! -e "$dropin" ] || fail "the autostart drop-in outlived the session: $(cat "$dropin")"
ok "and the drop-in it wrote is gone"

# Con --no-autostart no lo lanza, que es lo que pide un escritorio que lanza el
# suyo.
rm -f "$log"
run_session bspwm.desktop -- --no-autostart
sleep 3
if systemctl --user is-active -q "$unit"; then
    diagnose
    fail "uxsm start --no-autostart started the XDG autostart"
fi
[ "$(runs)" = 0 ] || fail "the autostart entry ran $(runs) times with --no-autostart"
ok "uxsm start --no-autostart does not start it"
stop_session

# Y esa opción la pone el generador de entradas, apoyándose en la tabla: la de
# un escritorio que lanza su propio autostart la lleva, y la de un gestor de
# ventanas, no. Xfce no está instalado en estas máquinas, así que su entrada sale
# de la tabla, que es lo que hay que pedir con --from-table.
out=$(uxsm entry --exec --from-table xfce) || fail "uxsm entry --exec --from-table xfce failed: $out"
case $out in
*"Exec=uxsm start --no-autostart"*) ok "the generated entry of a desktop carries --no-autostart" ;;
*) fail "the generated entry of xfce says: $out" ;;
esac
out=$(uxsm entry --exec bspwm) || fail "uxsm entry --exec bspwm failed: $out"
case $out in
*--no-autostart*) fail "the generated entry of bspwm carries --no-autostart: $out" ;;
*) ok "and the one of a window manager does not" ;;
esac
