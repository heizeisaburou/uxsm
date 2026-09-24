#!/bin/sh
# La sesión abierta por un display manager de verdad, no por la unidad pasajera
# de las demás pruebas: LightDM con autologin sobre Xvfb.
#
# Es lo único que prueba el camino real: PAM, logind creando la sesión, el
# XAUTHORITY que escribe LightDM, el DESKTOP_SESSION que deja puesto, la entrada
# generada leída desde el directorio de sesiones, y el cierre forzado cuando el
# display manager se lleva la sesión por delante.
#
# La configuración va donde LightDM la busca, no dentro de $HOME: en Fedora corre
# confinado por SELinux y leer su configuración desde el home de un usuario no es
# lo que su política permite. La máquina es desechable, así que configurar el
# LightDM del sistema es más sencillo y se parece más a lo que hay fuera.
#
# Va la última a propósito: instala LightDM, y las pruebas anteriores comprueban
# lo que hace uxsm en una máquina sin display manager.

set -eu
. "$(dirname "$0")/lib.sh"

dropin=/etc/lightdm/lightdm.conf.d/99-uxsm-test.conf
xserver=/usr/local/bin/uxsm-it-xserver
entry=/usr/local/share/xsessions/bspwm-uxsm.desktop
desktop=uxsm-desktop@bspwm.desktop.service

cleanup() {
    sudo systemctl stop lightdm.service 2>/dev/null || true
    uxsm stop 2>/dev/null || true
    sudo rm -f "$entry" "$dropin" "$xserver"
}
trap cleanup EXIT

wait_no_session || fail "a session from an earlier test is still shutting down"

echo "  installing LightDM"
install_log=$HOME/uxsm-it-lightdm-install.log
if command -v apt-get >/dev/null 2>&1; then
    # En Debian y Ubuntu, instalar un display manager lo arranca; policy-rc.d se
    # lo impide, que aquí lo arrancamos nosotros con nuestra configuración.
    printf '#!/bin/sh\nexit 101\n' | sudo tee /usr/sbin/policy-rc.d >/dev/null
    sudo chmod 755 /usr/sbin/policy-rc.d
    sudo DEBIAN_FRONTEND=noninteractive apt-get -y -qq install lightdm >"$install_log" 2>&1 || true
    sudo rm -f /usr/sbin/policy-rc.d
elif command -v pacman >/dev/null 2>&1; then
    sudo pacman -S --noconfirm --needed lightdm >"$install_log" 2>&1 || true
elif command -v dnf >/dev/null 2>&1; then
    sudo dnf -y -q install lightdm >"$install_log" 2>&1 || true
elif command -v zypper >/dev/null 2>&1; then
    sudo zypper --non-interactive --quiet install --no-recommends lightdm >"$install_log" 2>&1 || true
fi
sudo systemctl disable --now lightdm.service display-manager.service 2>/dev/null || true

# Sólo para saber si está instalado: se arranca por su unidad. En Debian,
# /usr/sbin no está en el PATH de un usuario normal, así que hay que buscar el
# programa donde cada distribución lo ponga.
lightdm=$(command -v lightdm 2>/dev/null || true)
if [ -z "$lightdm" ]; then
    for p in /usr/sbin/lightdm /usr/bin/lightdm /sbin/lightdm; do
        [ -x "$p" ] || continue
        lightdm=$p
        break
    done
fi
if [ -z "$lightdm" ]; then
    echo "  skipped: lightdm could not be installed here"
    tail -n 5 "$install_log" 2>/dev/null | sed 's/^/    /' || true
    rm -f "$install_log"
    exit 0
fi
rm -f "$install_log"

# El autologin de LightDM pasa por PAM, y en algunas distribuciones su fichero
# pide que el usuario esté en el grupo autologin.
sudo groupadd -f -r autologin 2>/dev/null || true
sudo gpasswd -a "$USER" autologin >/dev/null 2>&1 || true

# LightDM llama al servidor X como si fuera Xorg, con opciones de VT que Xvfb no
# entiende; este envoltorio se queda con lo que sí: el display y la cookie.
sudo tee "$xserver" >/dev/null <<'XSERVER'
#!/bin/sh
display=:0
auth=
while [ $# -gt 0 ]; do
    case $1 in
    :*) display=$1 ;;
    -auth)
        auth=$2
        shift
        ;;
    esac
    shift
done
if [ -n "$auth" ]; then
    exec Xvfb "$display" -screen 0 1280x800x24 -auth "$auth"
fi
exec Xvfb "$display" -screen 0 1280x800x24
XSERVER
sudo chmod 755 "$xserver"

sudo mkdir -p "$(dirname "$dropin")"
sudo tee "$dropin" >/dev/null <<CONF
[LightDM]
sessions-directory=/usr/local/share/xsessions
# Estas máquinas no tienen tarjeta gráfica, así que logind no da su asiento por
# gráfico y LightDM se quedaría esperando uno para siempre.
logind-check-graphical=false

[Seat:*]
xserver-command=$xserver
autologin-user=$USER
autologin-user-timeout=0
autologin-session=bspwm-uxsm
user-session=bspwm-uxsm
CONF

# La entrada que arranca la sesión es la que genera uxsm, en el directorio que
# sólo trae /usr/local: el camino entero de uxsm entry.
sudo uxsm entry -i bspwm >/dev/null 2>&1 || fail "uxsm entry -i bspwm failed"
[ -f "$entry" ] || fail "$entry was not installed"

# Sus directorios de trabajo: en Debian no están recién instalado el paquete, y
# sin ellos LightDM arranca, se queja y no llega a abrir ninguna sesión.
sudo mkdir -p /var/lib/lightdm/data /var/lib/lightdm-data /var/cache/lightdm /var/log/lightdm /run/lightdm
if id lightdm >/dev/null 2>&1; then
    sudo chown -R lightdm:lightdm /var/lib/lightdm /var/lib/lightdm-data \
        /var/cache/lightdm /var/log/lightdm /run/lightdm 2>/dev/null || true
fi

# Se arranca por su unidad, que es como corre en cualquier máquina.
sudo systemctl start lightdm.service || fail "lightdm did not start"

wait_for 60 systemctl --user is-active "$desktop" || {
    # Todo lo que puede decir algo: lo que systemd vio del display manager, lo
    # que escribió él, lo que dijo la sesión al morir y lo que sabe logind.
    sudo journalctl -u lightdm -n 20 --no-pager 2>/dev/null | sed 's/^/  journal: /' || true
    sudo tail -n 30 /var/log/lightdm/*.log 2>/dev/null | sed 's/^/  lightdm: /' || true
    tail -n 20 "$HOME/.xsession-errors" 2>/dev/null | sed 's/^/  xsession-errors: /' || true
    loginctl list-sessions --no-legend 2>/dev/null | sed 's/^/  loginctl: /' || true
    loginctl list-seats --no-legend 2>/dev/null | sed 's/^/  seats: /' || true
    fail "LightDM did not start the uxsm session"
}
wait_for 30 systemctl --user is-active graphical-session.target ||
    fail "the session started by LightDM is not ready"
ok "LightDM opens the session of the generated entry"

pid=$(systemctl --user show -p MainPID --value "$desktop")
env_of() { tr '\0' '\n' <"/proc/$pid/environ" | sed -n "s/^$1=//p"; }

[ "$(cat "/proc/$pid/comm")" = bspwm ] || fail "the main process is $(cat "/proc/$pid/comm"), not bspwm"

# La cookie es la que deje el display manager, y dónde la deja es cosa suya: en
# Debian y Ubuntu, el wrapper de sesión la copia a ~/.Xauthority. Lo que importa
# es que el escritorio la recibió y sirve: con ella se conectaron al servidor X
# tanto bspwm como la espera de uxsm, que es lo que activó la sesión gráfica.
xauth=$(env_of XAUTHORITY)
[ -n "$xauth" ] && [ -f "$xauth" ] ||
    fail "the desktop has XAUTHORITY='$xauth', which is not a file that exists"
ok "the desktop got a working XAUTHORITY ($xauth) on DISPLAY=$(env_of DISPLAY)"

[ "$(env_of DESKTOP_SESSION)" = bspwm-uxsm ] ||
    fail "DESKTOP_SESSION is $(env_of DESKTOP_SESSION), expected bspwm-uxsm"
ok "and DESKTOP_SESSION says the session is uxsm's"

# La sesión es de logind, no un proceso suelto: es lo que decide lo que puede
# hacer polkit y lo que ve el resto del sistema. Se le pregunta a logind y no al
# entorno del escritorio: XDG_SESSION_ID es de la sesión de login, y uxsm no lo
# sube al gestor de systemd, que es de todo el usuario y no de una sesión
# (internal/sessionenv, sessionSpecific).
session=""
for id in $(loginctl list-sessions --no-legend | awk '{print $1}'); do
    case $(loginctl show-session "$id" -p Service --value 2>/dev/null) in
    lightdm*) session=$id ;;
    esac
done
[ -n "$session" ] || fail "logind has no session opened by lightdm: $(loginctl list-sessions --no-legend)"

type=$(loginctl show-session "$session" -p Type --value)
[ "$type" = x11 ] || fail "the logind session of LightDM is of type '$type', expected x11"
leader=$(loginctl show-session "$session" -p Leader --value)
ok "logind has an x11 session from LightDM, led by $(tr '\0' ' ' <"/proc/$leader/cmdline" | cut -c1-60)"

# Y el cierre que sólo hace un display manager: se lleva la sesión por delante.
sudo systemctl stop lightdm.service
wait_stopped 30 "$desktop" uxsm-env@bspwm.desktop.service graphical-session.target ||
    fail "the session outlived the display manager"
ok "stopping the display manager shuts the whole session down"
