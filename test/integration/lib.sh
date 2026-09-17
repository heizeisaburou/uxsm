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

# start_xvfb :N arranca un servidor X sin pantalla en el display :N, como unidad
# pasajera de systemd --user, y espera a que acepte conexiones.
start_xvfb() {
    n=${1#:}
    systemd-run --user --quiet --collect --unit="uxsm-it-xvfb-$n" Xvfb ":$n" -screen 0 1280x800x24
    wait_for 10 test -S "/tmp/.X11-unix/X$n" || fail "Xvfb :$n did not start"
}
