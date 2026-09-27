#!/bin/sh
# A session opened by a real display manager rather than the transient unit used
# elsewhere: LightDM with autologin on Xvfb.
#
# This alone tests the real path: PAM, logind creating the session, XAUTHORITY
# written by LightDM, DESKTOP_SESSION left by it, the generated entry read from
# the session directory, and forced shutdown when the display manager terminates
# the session.
#
# Configuration goes where LightDM expects it, not under $HOME: on Fedora it is
# confined by SELinux, whose policy does not permit reading configuration from a
# user's home. The VM is disposable, so configuring system LightDM is simpler
# and closer to a real installation.
#
# This deliberately runs last because it installs LightDM, while earlier tests
# verify uxsm on a machine without a display manager.

set -eu
. "$(dirname "$0")/lib.sh"

dropin=/etc/lightdm/lightdm.conf.d/99-uxsm-test.conf
ours=/etc/lightdm/lightdm.conf.d/99-uxsm.conf
xserver=/usr/local/bin/uxsm-it-xserver
entry=/usr/local/share/xsessions/bspwm-uxsm.desktop
desktop=uxsm-desktop@bspwm.desktop.service

cleanup() {
    sudo systemctl stop lightdm.service 2>/dev/null || true
    uxsm stop 2>/dev/null || true
    sudo rm -f "$entry" "$dropin" "$ours" "$xserver"
}
trap cleanup EXIT

wait_no_session || fail "a session from an earlier test is still shutting down"

echo "  installing LightDM"
install_log=$HOME/uxsm-it-lightdm-install.log
if command -v apt-get >/dev/null 2>&1; then
    # Debian and Ubuntu start a display manager when installing it; policy-rc.d
    # prevents that because this test starts it with its own configuration.
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

# This only determines whether it is installed; startup uses its unit. On Debian,
# /usr/sbin is absent from a normal user's PATH, so search wherever each
# distribution installs the program.
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

# LightDM autologin goes through PAM, and some distributions require membership
# in the autologin group.
sudo groupadd -f -r autologin 2>/dev/null || true
sudo gpasswd -a "$USER" autologin >/dev/null 2>&1 || true

# LightDM invokes the X server as if it were Xorg, with VT options Xvfb does not
# understand; this wrapper retains only the supported display and cookie.
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
# These VMs have no graphics card, so logind does not mark their seat graphical
# and LightDM would otherwise wait forever.
logind-check-graphical=false

[Seat:*]
xserver-command=$xserver
autologin-user=$USER
autologin-user-timeout=0
autologin-session=bspwm-uxsm
user-session=bspwm-uxsm
CONF

# Against a real LightDM, uxsm setup also makes it read the Wayland directory:
# it changes the line in the file that sets the option, here the test file, and
# the session still starts. It does not restart LightDM.
out=$(sudo uxsm setup sessions-dir -i lightdm) || fail "uxsm setup sessions-dir -i failed: $out"
case $out in
*"systemctl restart lightdm.service"*) ok "uxsm setup sessions-dir -i says how to restart LightDM" ;;
*) fail "uxsm setup sessions-dir -i did not say how to restart it: $out" ;;
esac
if systemctl is-active --quiet lightdm.service; then
    fail "uxsm restarted the display manager by itself"
fi
ok "and does not restart it by itself"
case $(uxsm setup sessions-dir lightdm) in
*"nothing to do"*) ok "and leaves both local directories read" ;;
*) fail "after setup, LightDM still does not read them: $(uxsm setup sessions-dir lightdm)" ;;
esac

# The session starts from the entry generated by uxsm in the directory present
# only under /usr/local, exercising the complete uxsm entry path.
sudo uxsm entry -i bspwm >/dev/null 2>&1 || fail "uxsm entry -i bspwm failed"
[ -f "$entry" ] || fail "$entry was not installed"

# LightDM's working directories are absent after installation on Debian; without
# them it starts, complains, and never opens a session.
sudo mkdir -p /var/lib/lightdm/data /var/lib/lightdm-data /var/cache/lightdm /var/log/lightdm /run/lightdm
if id lightdm >/dev/null 2>&1; then
    sudo chown -R lightdm:lightdm /var/lib/lightdm /var/lib/lightdm-data \
        /var/cache/lightdm /var/log/lightdm /run/lightdm 2>/dev/null || true
fi

# Start it through its unit, as on a normal machine.
sudo systemctl start lightdm.service || fail "lightdm did not start"

wait_for 60 systemctl --user is-active "$desktop" || {
    # Every useful diagnostic: systemd's view of the display manager, its own
    # output, the session's final output, and logind state.
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

# The display manager chooses the cookie and its location; on Debian and Ubuntu,
# the session wrapper copies it to ~/.Xauthority before launch. What matters is that the
# desktop received a working cookie: both bspwm and uxsm's readiness wait used
# it to connect to the X server, which activated the graphical session.
xauth=$(env_of XAUTHORITY)
[ -n "$xauth" ] && [ -f "$xauth" ] ||
    fail "the desktop has XAUTHORITY='$xauth', which is not a file that exists"
ok "the desktop got a working XAUTHORITY ($xauth) on DISPLAY=$(env_of DISPLAY)"

[ "$(env_of DESKTOP_SESSION)" = bspwm-uxsm ] ||
    fail "DESKTOP_SESSION is $(env_of DESKTOP_SESSION), expected bspwm-uxsm"
ok "and DESKTOP_SESSION says the session is uxsm's"

# This is a logind session, not a standalone process; that determines polkit
# permissions and system-wide visibility. Query logind rather than the desktop
# environment: XDG_SESSION_ID belongs to the login session, and uxsm does not
# import it into the per-user systemd manager (internal/sessionenv,
# sessionSpecific).
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

# Finally, test display-manager-only shutdown by terminating the whole session.
sudo systemctl stop lightdm.service
wait_stopped 30 "$desktop" uxsm-env@bspwm.desktop.service graphical-session.target ||
    fail "the session outlived the display manager"
ok "stopping the display manager shuts the whole session down"
