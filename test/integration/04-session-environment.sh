#!/bin/sh
# Paso 3b: uxsm-env@.service monta el entorno de la sesión en el gestor antes del
# escritorio ―perfil, ficheros uxsm/env y env-<escritorio> con sus .d― y al
# cerrar deja el entorno del gestor exactamente como estaba, venga de donde venga
# el cierre.

set -eu
. "$(dirname "$0")/lib.sh"

env_unit=uxsm-env@uxsm-it-names.desktop.service
config=$HOME/.config/uxsm

make_test_entries
mkdir -p "$config/env.d"
printf 'export UXSM_IT_COMMON=common\n' >"$config/env"
printf 'export UXSM_IT_DESKTOP=testde\n' >"$config/env-testde"
printf 'export UXSM_IT_DROPIN=dropin\n' >"$config/env.d/10-it"
printf 'export UXSM_IT_OTHER=other\n' >"$config/env-other"

cleanup() {
    systemctl --user stop uxsm-it-session.service uxsm-it-xvfb-5.service 2>/dev/null || true
    systemctl --user unset-environment WAYLAND_DISPLAY 2>/dev/null || true
    remove_test_entries
    rm -rf "$config"
}
trap cleanup EXIT

# snapshot: el entorno completo del gestor, ordenado. Dos fotos iguales con el
# mismo formato se comparan bien aunque systemctl escape algunos valores.
snapshot() {
    systemctl --user show-environment | sort
}

start_xvfb :5

# Basura de una sesión anterior que no limpió: la sesión no debe verla, y al
# cerrar tiene que volver, porque estaba en la foto.
systemctl --user set-environment WAYLAND_DISPLAY=wayland-stale
before=$(snapshot)

for close in "quitting bspwm" "killing the session process" "uxsm stop"; do
    run_session uxsm-it-names.desktop --

    systemctl --user is-active --quiet "$env_unit" graphical-session-pre.target ||
        fail "$env_unit or graphical-session-pre.target is not active"
    expect_var UXSM_IT_COMMON common
    expect_var UXSM_IT_DROPIN dropin
    expect_var UXSM_IT_DESKTOP testde
    expect_var UXSM_IT_OTHER ""
    expect_var DISPLAY :5
    expect_var WAYLAND_DISPLAY ""
    expect_var XDG_SESSION_TYPE x11

    case $close in
    "quitting bspwm") DISPLAY=:5 bspc quit ;;
    "killing the session process") systemctl --user kill --signal=TERM uxsm-it-session.service ;;
    "uxsm stop") uxsm stop ;;
    esac
    wait_stopped 15 uxsm-it-session.service "$desktop" "$env_unit" ||
        fail "after $close the session is still up"

    after=$(snapshot)
    if [ "$after" != "$before" ]; then
        printf '%s\n' "$before" >/tmp/uxsm-it-before
        printf '%s\n' "$after" >/tmp/uxsm-it-after
        fail "after $close the systemd environment changed: $(diff /tmp/uxsm-it-before /tmp/uxsm-it-after | tr '\n' ' ')"
    fi
    ok "with $close: the desktop gets env, env.d and env-testde, no stale WAYLAND_DISPLAY, and the environment is restored"
done

[ ! -e "$XDG_RUNTIME_DIR/uxsm/env_pre" ] || fail "env_pre is still in \$XDG_RUNTIME_DIR/uxsm"
ok "the session files are removed at the end"
