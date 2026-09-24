# Funciones comunes de las pruebas de integración. Se cargan con `. lib.sh`.

# ok y fail informan de cada comprobación; fail además termina la prueba.
ok() { echo "  ok: $*"; }
fail() { echo "  FAIL: $*"; exit 1; }

# wait_for SEGUNDOS COMANDO...: repite el comando cada décima de segundo hasta
# que termine bien. Devuelve error si se acaba el tiempo.
wait_for() {
    limit=$(( $1 * 10 ))
    shift
    i=0
    until "$@" >/dev/null 2>&1; do
        i=$((i + 1))
        [ "$i" -lt "$limit" ] || return 1
        sleep 0.1
    done
}

# wait_stopped SEGUNDOS UNIDADES...: espera a que ninguna esté activa, arrancando
# o parando. `systemctl is-active` falla también con una unidad que se está
# parando, y sus ExecStopPost= pueden seguir corriendo.
wait_stopped() {
    limit=$1
    shift
    wait_for "$limit" sh -c '! systemctl --user is-active "$@" | grep -qvx -e inactive -e failed' sh "$@"
}

# wait_no_session SEGUNDOS: espera a que no quede viva ninguna unidad de una
# sesión de uxsm. No basta con que se haya parado el escritorio: la limpieza del
# entorno corre en el ExecStopPost= de uxsm-env@, y hasta que termina siguen ahí
# los ficheros de la sesión.
wait_no_session() {
    wait_for "${1:-15}" sh -c 'test -z "$(systemctl --user list-units --state=active,activating,deactivating --no-legend "uxsm-desktop@*.service" "uxsm-env@*.service" "uxsm-session@*.target" "uxsm-bindpid@*.service" uxsm-shutdown.target)"'
}

# start_xvfb :N arranca un servidor X sin pantalla en el display :N, como unidad
# pasajera de systemd --user, y espera a que acepte conexiones.
start_xvfb() {
    n=${1#:}
    # El nombre lleva xvfb a propósito: en shell las variables de una función
    # son las de todos, y las pruebas ya usan "unit" para lo suyo.
    xvfb_unit=uxsm-it-xvfb-$n.service
    # La prueba anterior lo paró hace un instante, y hasta que systemd lo suelta
    # del todo sigue conociendo ese nombre: systemd-run se negaría a usarlo.
    systemctl --user stop "$xvfb_unit" 2>/dev/null || true
    systemctl --user reset-failed "$xvfb_unit" 2>/dev/null || true
    wait_for 15 sh -c "[ \"\$(systemctl --user show -p LoadState --value $xvfb_unit)\" = not-found ]" ||
        fail "$xvfb_unit is still known to systemd: $(systemctl --user show -p LoadState -p ActiveState --value "$xvfb_unit" | tr '\n' ' ')"
    systemd-run --user --quiet --collect --unit="uxsm-it-xvfb-$n" Xvfb ":$n" -screen 0 1280x800x24
    wait_for 10 test -S "/tmp/.X11-unix/X$n" || fail "Xvfb :$n did not start"
}

# make_test_entries crea en ~/.local/share/xsessions dos entradas de sesión
# propias, iguales en todas las distribuciones: uxsm-it-names.desktop, con
# DesktopNames=TestDE;Second;, y uxsm-it-nonames.desktop, sin DesktopNames=. Las
# dos ejecutan bspwm. remove_test_entries las borra.
make_test_entries() {
    entries=$HOME/.local/share/xsessions
    mkdir -p "$entries"
    cat >"$entries/uxsm-it-names.desktop" <<ENTRY
[Desktop Entry]
Type=Application
Name=uxsm test, with DesktopNames
Exec=bspwm
DesktopNames=TestDE;Second;
ENTRY
    cat >"$entries/uxsm-it-nonames.desktop" <<ENTRY
[Desktop Entry]
Type=Application
Name=uxsm test, without DesktopNames
Exec=bspwm
ENTRY
}

remove_test_entries() {
    rm -f "$HOME/.local/share/xsessions/uxsm-it-names.desktop" \
        "$HOME/.local/share/xsessions/uxsm-it-nonames.desktop"
}

# run_session ENTRADA [VAR=valor…] -- [ARGS…]: arranca la sesión de ENTRADA como
# lo haría el display manager, en :5, con esas variables en su entorno y esos
# argumentos para uxsm start, y espera a que esté entera. Deja en $desktop el
# nombre de la unidad del escritorio.
#
# El proceso de sesión es la unidad pasajera uxsm-it-session. Una unidad de
# systemd-run hereda el entorno del gestor, y un display manager no: por eso
# uxsm start pasa por `env -u XDG_CURRENT_DESKTOP`, para no recibir el que haya
# quedado en el gestor.
run_session() {
    entry=$1
    shift
    assignments=""
    while [ "$1" != "--" ]; do
        assignments="$assignments $1"
        shift
    done
    shift
    # shellcheck disable=SC2086
    systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 \
        env -u XDG_CURRENT_DESKTOP $assignments uxsm start "$@" "$entry"
    desktop=uxsm-desktop@$entry.service
    wait_for 15 systemctl --user is-active graphical-session.target ||
        fail "the session of $entry did not start with: $*"
}

# stop_session cierra la sesión con uxsm stop y espera a que no quede nada.
stop_session() {
    uxsm stop
    wait_stopped 15 uxsm-it-session.service "$desktop" ||
        fail "the session did not stop"
}

# expect_var NOMBRE VALOR: el proceso del escritorio tiene esa variable con ese
# valor. Con VALOR vacío, que no la tiene o la tiene vacía.
expect_var() {
    pid=$(systemctl --user show -p MainPID --value "$desktop")
    got=$(tr '\0' '\n' <"/proc/$pid/environ" | sed -n "s/^$1=//p")
    [ "$got" = "$2" ] || fail "$1 is '$got', expected '$2'"
}
