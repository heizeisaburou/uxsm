#!/bin/sh
# Step 5: XDG autostart. uxsm activates xdg-desktop-autostart.target, and entries
# in ~/.config/autostart start once as app-<name>@autostart.service and stop with
# the session. Like uwsm, uxsm always does this unless `uxsm start --no-autostart`
# disables it; generated entries use that option for desktops that launch their
# own autostart.

set -eu
. "$(dirname "$0")/lib.sh"

probe=$HOME/uxsm-it-probe
log=$HOME/uxsm-it-autostart.log
entry=$HOME/.config/autostart/uxsm-it-probe.desktop
unit="app-$(systemd-escape uxsm-it-probe)@autostart.service"
target=uxsm-autostart@bspwm.desktop.target
dropin=$XDG_RUNTIME_DIR/systemd/user/app-@autostart.service.d/uxsm-tweaks.conf

# diagnose prints autostart unit state after a failure, avoiding a manual search
# inside the VM.
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

# The probe records every execution and stays alive like an applet.
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
# systemd's generator creates the unit only if the executable exists, so the
# probe must be written first.
systemctl --user daemon-reload

runs() { [ -f "$log" ] && wc -l <"$log" || echo 0; }

start_xvfb :5

# A known window manager: uxsm owns autostart.
run_session bspwm.desktop --
wait_for 15 systemctl --user is-active "$unit" || {
    diagnose
    fail "the autostart entry did not start with the session"
}
ok "with a window manager, uxsm starts the XDG autostart"

# The unit becomes active as soon as systemd executes the entry, before it records
# anything, so wait for its trace and then a little longer, when a second launch
# would appear.
wait_for 15 test -s "$log" || fail "the autostart entry did not run"
sleep 2
[ "$(runs)" = 1 ] || fail "the autostart entry ran $(runs) times, expected once"
ok "and its entry runs exactly once"

# It belongs to uxsm's slice, not app.slice where the generator leaves it; the
# drop-in written by uxsm when autostart begins makes that change.
slice=$(systemctl --user show -p Slice --value "$unit")
[ "$slice" = app-uxsm.slice ] || fail "the autostart entry is in $slice, not app-uxsm.slice"
ok "in the applications slice of the session, not in app.slice"

grep -q bspwm "$log" || fail "the autostart entry did not get XDG_CURRENT_DESKTOP=bspwm: $(cat "$log")"
ok "with XDG_CURRENT_DESKTOP, which is what filters OnlyShowIn="

stop_session
wait_stopped 15 "$unit" xdg-desktop-autostart.target || fail "the autostart entry outlived the session"
ok "and it stops with the session"

# The drop-in belongs to the session, not the user: after shutdown nothing
# changes the slice of later sessions, including uwsm sessions.
[ ! -e "$dropin" ] || fail "the autostart drop-in outlived the session: $(cat "$dropin")"
ok "and the drop-in it wrote is gone"

# With --no-autostart, uxsm does not launch it, as required by a desktop that
# launches its own.
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

# The entry generator adds that option using the table: an entry for a desktop
# that launches its own autostart includes it, while a window manager entry does
# not. Xfce is not installed in these VMs, so its entry comes from the table,
# explicitly requested with --from-table.
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
