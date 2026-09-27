#!/bin/sh
# Step 1: `uxsm start` launches the entry's Exec= as a systemd --user service,
# and the session lasts as long as the desktop. The same applies to a command
# instead of an entry: `uxsm start -- bspwm`.
#
# A transient unit, uxsm-it-session, stands in for the display manager and runs
# `uxsm start` with DISPLAY set, as LightDM would.

set -eu
. "$(dirname "$0")/lib.sh"

unit=uxsm-desktop@bspwm.desktop.service
cmd_unit=uxsm-desktop@bspwm.service

cleanup() {
    systemctl --user stop uxsm-it-session.service "$unit" "$cmd_unit" uxsm-it-xvfb-5.service 2>/dev/null || true
}
trap cleanup EXIT

if uxsm start missing.desktop 2>/dev/null; then
    fail "uxsm start with a missing entry did not fail"
fi
ok "a missing entry is an error"

if err=$(DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent uxsm start bspwm.desktop 2>&1); then
    fail "uxsm start without a session bus did not fail"
fi
case $err in
*"no D-Bus session bus"*) ok "no session bus is an error that says so" ;;
*) fail "uxsm start without a session bus: $err" ;;
esac

start_xvfb :5
systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 uxsm start bspwm.desktop

wait_for 15 systemctl --user is-active "$unit" || fail "$unit is not active"
ok "$unit is active"

main=$(systemctl --user show -p MainPID --value "$unit")
comm=$(cat "/proc/$main/comm")
[ "$comm" = bspwm ] || fail "the main process of $unit is $comm, not bspwm"
ok "its main process is bspwm itself"

systemctl --user show-environment | grep -qx 'DISPLAY=:5' || fail "DISPLAY=:5 is not in the systemd environment"
ok "DISPLAY=:5 reached the systemd environment"

wait_for 10 sh -c 'DISPLAY=:5 xprop -root _NET_SUPPORTING_WM_CHECK | grep -q "window id"' ||
    fail "bspwm is not managing :5"
ok "bspwm manages the display"

DISPLAY=:5 bspc quit
wait_for 10 sh -c '! systemctl --user is-active uxsm-it-session.service' ||
    fail "the session process is still running after bspc quit"
ok "quitting bspwm ends the session process"

# The same session started from a command. The instance is the program name,
# and uxsm aux exec reads the command saved by uxsm start.
if uxsm start -- uxsm-it-no-such-program 2>/dev/null; then
    fail "uxsm start with a missing program did not fail"
fi
ok "a missing program is an error"

wait_stopped 15 uxsm-it-session.service "$unit" || fail "the entry session did not stop"
systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 uxsm start -- bspwm

wait_for 15 systemctl --user is-active "$cmd_unit" || fail "$cmd_unit is not active"
ok "uxsm start -- bspwm starts $cmd_unit"

main=$(systemctl --user show -p MainPID --value "$cmd_unit")
comm=$(cat "/proc/$main/comm")
[ "$comm" = bspwm ] || fail "the main process of $cmd_unit is $comm, not bspwm"
ok "its main process is bspwm itself"

wait_for 10 sh -c 'DISPLAY=:5 xprop -root _NET_SUPPORTING_WM_CHECK | grep -q "window id"' ||
    fail "bspwm is not managing :5"
DISPLAY=:5 bspc quit
wait_stopped 15 uxsm-it-session.service "$cmd_unit" uxsm-env@bspwm.service ||
    fail "the command session did not stop after bspc quit"
ok "quitting bspwm ends the command session"

[ ! -e "$XDG_RUNTIME_DIR/uxsm/command" ] || fail "the saved command is still there after the session"
ok "the saved command is removed at the end"
