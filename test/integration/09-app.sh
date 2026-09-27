#!/bin/sh
# uxsm app: each application gets its own unit in the session slices. Verify the
# generated unit name, requested slice, execution of a .desktop entry's Exec=
# and requested action, and the purpose of the mechanism: applications stop with
# the session.

set -eu
. "$(dirname "$0")/lib.sh"

apps=$HOME/.local/share/applications
entry=$apps/uxsm-it-app.desktop
term=$apps/uxsm-it-term.desktop

cleanup() {
    uxsm stop 2>/dev/null || true
    systemctl --user stop uxsm-it-session.service uxsm-it-xvfb-5.service 2>/dev/null || true
    systemctl --user stop 'app-uxsm-*' 2>/dev/null || true
    rm -f "$entry" "$term"
    remove_test_entries
}
trap cleanup EXIT

wait_no_session || fail "a session from an earlier test is still shutting down"

mkdir -p "$apps"
cat >"$entry" <<ENTRY
[Desktop Entry]
Type=Application
Name=uxsm integration test app
Exec=sleep 3000 %U
Icon=uxsm-it

[Desktop Action alt]
Name=the other one
Exec=sleep 3001
ENTRY
cat >"$term" <<ENTRY
[Desktop Entry]
Type=Application
Name=uxsm integration test terminal app
Exec=sleep 3002
Terminal=true
ENTRY

start_xvfb :5
make_test_entries
run_session uxsm-it-names.desktop --

# unit_of PATTERN: the active unit name matching the pattern.
unit_of() {
    systemctl --user list-units --state=active --no-legend "$1" | awk '{print $1}' | head -1
}
slice_of() { systemctl --user show -p Slice --value "$1"; }

# is-active reports true inside the session; the final check covers outside it.
uxsm check is-active || fail "uxsm check is-active says there is no session, inside one"
uxsm check is-active -v | grep -q "uxsm-desktop@" || fail "uxsm check is-active -v does not list the desktop unit"
ok "uxsm check is-active says there is a session, and -v says which units"

# A direct command as a service so the test does not remain attached to it.
uxsm app -t service -- sleep 3000 || fail "uxsm app with a command failed"
unit=$(unit_of 'app-uxsm-sleep@*.service')
[ -n "$unit" ] || fail "no unit was created for the command"
ok "a command runs in its own unit ($unit)"
[ "$(slice_of "$unit")" = app-uxsm.slice ] ||
    fail "the unit is in $(slice_of "$unit"), not app-uxsm.slice"
ok "and in the applications slice of the session"

# -s selects the slice.
uxsm app -t service -s b -a background -- sleep 3000 || fail "uxsm app -s b failed"
unit=$(unit_of 'app-uxsm-background@*.service')
[ -n "$unit" ] || fail "no unit was created with -s b"
[ "$(slice_of "$unit")" = background-uxsm.slice ] ||
    fail "with -s b the unit is in $(slice_of "$unit"), not background-uxsm.slice"
ok "-s b puts it in the background slice"

# -p passes systemd properties to the unit, as systemd-run does.
uxsm app -t service -a props -p MemoryMax=512M -p TimeoutStopSec=5 -- sleep 3000 ||
    fail "uxsm app -p failed"
unit=$(unit_of 'app-uxsm-props@*.service')
[ -n "$unit" ] || fail "no unit was created with -p"
[ "$(systemctl --user show -p MemoryMax --value "$unit")" = 536870912 ] ||
    fail "MemoryMax is $(systemctl --user show -p MemoryMax --value "$unit"), not 512M"
[ "$(systemctl --user show -p TimeoutStopUSec --value "$unit")" = 5s ] ||
    fail "TimeoutStopSec is $(systemctl --user show -p TimeoutStopUSec --value "$unit"), not 5s"
ok "-p passes the properties to the unit"

if err=$(uxsm app -t service -p MemoryMax -- sleep 3000 2>&1); then
    fail "a property without a value was accepted"
fi
case $err in
*"Key=Value"*) ok "and a property that is not Key=Value says so" ;;
*) fail "uxsm app -p MemoryMax: $err" ;;
esac

# For an application entry, derive the unit name from the entry and run Exec=
# with resolved field codes.
uxsm app -t service uxsm-it-app.desktop || fail "uxsm app with a desktop entry failed"
unit=$(unit_of 'app-uxsm-uxsm-it-app@*.service')
[ -n "$unit" ] || fail "no unit was created for the desktop entry"
ok "a desktop entry runs in a unit named after it ($unit)"
[ "$(systemctl --user show -p Description --value "$unit")" = "uxsm integration test app" ] ||
    fail "the unit description is not the Name= of the entry"
ok "with the name of the entry as description"

# Also test its actions.
uxsm app -t service -a action uxsm-it-app.desktop:alt || fail "uxsm app with an action failed"
unit=$(unit_of 'app-uxsm-action@*.service')
[ -n "$unit" ] || fail "no unit was created for the action"
pid=$(systemctl --user show -p MainPID --value "$unit")
tr '\0' ' ' <"/proc/$pid/cmdline" | grep -q 3001 ||
    fail "the action ran $(tr '\0' ' ' <"/proc/$pid/cmdline"), not its own Exec="
ok "an action runs its own Exec="

if err=$(uxsm app -t service uxsm-it-app.desktop:nope 2>&1); then
    fail "an action that does not exist was accepted"
fi
case $err in
*"has no action"*) ok "and an action that does not exist says so" ;;
*) fail "uxsm app with a missing action: $err" ;;
esac

# Unsupported behavior is reported instead of being launched incorrectly.
if err=$(uxsm app -t service uxsm-it-term.desktop 2>&1); then
    fail "an entry with Terminal=true was accepted"
fi
case $err in
*"inside a terminal"*) ok "an entry that wants a terminal says it is not supported yet" ;;
*) fail "uxsm app with Terminal=true: $err" ;;
esac

# A scope, the default: the application remains attached to its launcher.
uxsm app -a scoped -p TimeoutStopSec=5 -- sleep 3000 &
wait_for 10 sh -c 'systemctl --user list-units --state=active --no-legend "app-uxsm-scoped-*.scope" | grep -q .' ||
    fail "no scope was created"
ok "without -t, the application runs in a scope"
[ "$(systemctl --user show -p TimeoutStopUSec --value "$(unit_of 'app-uxsm-scoped-*.scope')")" = 5s ] ||
    fail "a scope did not take the TimeoutStopSec of -p"
ok "and a scope takes properties too"

# The reason for all of this: applications stop with the session.
stop_session
wait_for 15 sh -c '! systemctl --user list-units --state=active --no-legend "app-uxsm-*" | grep -q .' ||
    fail "the applications outlived the session: $(systemctl --user list-units --state=active --no-legend 'app-uxsm-*')"
ok "the applications stop with the session"

if uxsm check is-active; then
    fail "uxsm check is-active says there is a session after it stopped"
fi
ok "and is-active says there is no session any more"
