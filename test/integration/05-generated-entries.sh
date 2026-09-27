#!/bin/sh
# uxsm entry: generated entries start the session with the correct desktop names
# and neither overwrite nor shadow other entries without -f. Also test uxsm
# check and uxsm setup sessions-dir on a machine without a display manager.
#
# As in the other tests, a transient unit stands in for the display manager and
# runs Exec= from the generated entry. It omits XDG_CURRENT_DESKTOP, like a
# display manager that does not propagate the entry's DesktopNames=: the worst
# case, where names arrive only through Exec=.

set -eu
. "$(dirname "$0")/lib.sh"

dir=/usr/local/share/xsessions

cleanup() {
    systemctl --user stop uxsm-it-session.service uxsm-desktop@bspwm.desktop.service \
        uxsm-desktop@bspwm.service uxsm-it-xvfb-5.service 2>/dev/null || true
    sudo rm -f "$dir/bspwm-uxsm.desktop" "$dir/bspwm.desktop"
}
trap cleanup EXIT

# run_entry ENTRY UNIT: start the session with ENTRY's Exec= and wait for UNIT,
# the desktop unit, to become active. Store the unit in $desktop.
run_entry() {
    exec_line=$(sed -n 's/^Exec=//p' "$dir/$1")
    systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 \
        env -u XDG_CURRENT_DESKTOP sh -c "exec $exec_line"
    desktop=$2
    wait_for 15 systemctl --user is-active "$desktop" || fail "$1 did not start $desktop"
    wait_for 10 sh -c 'DISPLAY=:5 xprop -root _NET_SUPPORTING_WM_CHECK | grep -q "window id"' ||
        fail "bspwm is not managing :5"
}

# quit_session: exit bspwm and wait until no session unit remains.
quit_session() {
    DISPLAY=:5 bspc quit
    wait_stopped 15 uxsm-it-session.service "$desktop" || fail "the session of $desktop did not stop"
}

out=$(uxsm check) || fail "uxsm check without a display manager failed: $out"
case $out in
*"sessions dirs: unknown: no display manager"*) ok "uxsm check says it cannot tell without a display manager" ;;
*) fail "uxsm check: $out" ;;
esac
if uxsm setup sessions-dir lightdm >/dev/null 2>&1; then
    fail "uxsm setup sessions-dir lightdm worked without LightDM installed"
fi
ok "uxsm setup sessions-dir refuses a display manager that is not installed"

# Without -i, only show what would be written.
sudo uxsm entry bspwm 2>/dev/null | grep -q "^  $dir/bspwm-uxsm.desktop$" ||
    fail "uxsm entry without -i did not show what it would write"
[ ! -e "$dir/bspwm-uxsm.desktop" ] || fail "uxsm entry without -i wrote the entry"
ok "without -i, uxsm entry only shows the entry"

start_xvfb :5

# The entry that points to bspwm.desktop.
sudo uxsm entry -i bspwm >/dev/null 2>&1 || fail "uxsm entry -i bspwm failed"
run_entry bspwm-uxsm.desktop uxsm-desktop@bspwm.desktop.service
expect_var XDG_CURRENT_DESKTOP bspwm
ok "bspwm-uxsm.desktop starts bspwm.desktop as bspwm"
quit_session

# The direct-command entry: it does not overwrite the previous one without -f.
if sudo uxsm entry -i --exec bspwm 2>/dev/null; then
    fail "uxsm entry overwrote bspwm-uxsm.desktop without -f"
fi
ok "uxsm entry does not overwrite an entry without -f"
sudo uxsm entry -i -f --exec bspwm >/dev/null 2>&1 || fail "uxsm entry -i -f --exec bspwm failed"
run_entry bspwm-uxsm.desktop uxsm-desktop@bspwm.service
expect_var XDG_CURRENT_DESKTOP bspwm
ok "bspwm-uxsm.desktop with the command starts bspwm directly, as bspwm"
quit_session

# The entry source is explicit: --from-table uses the table without consulting
# the installed entry, while --no-table prevents the table from filling gaps.
out=$(sudo uxsm entry --from-table --exec -f bspwm) || fail "uxsm entry --from-table failed: $out"
case $out in
*"Exec=uxsm start -D bspwm -- bspwm"*) ok "--from-table takes the entry from uxsm's table" ;;
*) fail "uxsm entry --from-table --exec bspwm: $out" ;;
esac
if err=$(sudo uxsm entry --from-table -f bspwm 2>&1); then
    fail "--from-table was accepted without --exec or --plain"
fi
case $err in
*"needs --exec or --plain"*) ok "and says so when it cannot come from the table" ;;
*) fail "uxsm entry --from-table bspwm: $err" ;;
esac
# A plain entry, which can only come from the table, requires it explicitly.
if err=$(sudo uxsm entry --plain -f bspwm 2>&1); then
    fail "--plain without --from-table was accepted"
fi
case $err in
*"--plain --from-table"*) ok "and --plain asks where the entry comes from" ;;
*) fail "uxsm entry --plain bspwm: $err" ;;
esac
# Table-supplied data is tested through a command, which is identical on every
# distribution: bspwm is in the table, so its names come from there.
out=$(sudo uxsm entry --exec -f -- bspwm) || fail "uxsm entry --exec -- bspwm failed: $out"
case $out in
*"Exec=uxsm start -D bspwm -- bspwm"*) ok "the table fills in the desktop names a command does not have" ;;
*) fail "uxsm entry --exec -- bspwm: $out" ;;
esac
if err=$(sudo uxsm entry --no-table --exec -f -- bspwm 2>&1); then
    fail "--no-table took the desktop names from the table anyway"
fi
case $err in
*"no desktop names known"*) ok "--no-table does not fill them in, and asks for -D" ;;
*) fail "uxsm entry --no-table --exec -- bspwm: $err" ;;
esac
out=$(sudo uxsm entry --no-table -D MiWM --exec -f -- bspwm) ||
    fail "uxsm entry --no-table -D MiWM failed: $out"
case $out in
*"Exec=uxsm start -D MiWM -- bspwm"*) ok "and with -D the names are exactly the ones given" ;;
*) fail "uxsm entry --no-table -D MiWM --exec -- bspwm: $out" ;;
esac
if err=$(sudo uxsm entry --no-table --from-table --exec -f bspwm 2>&1); then
    fail "--from-table with --no-table was accepted"
fi
ok "and the two of them together are refused"

# -e retains names from -D and discards the rest, as in uxsm start.
out=$(sudo uxsm entry -e -D MiWM -f bspwm) || fail "uxsm entry -e failed: $out"
case $out in
*"DesktopNames=MiWM;"*) ok "-e keeps only the desktop names given with -D" ;;
*) fail "uxsm entry -e -D MiWM bspwm: $out" ;;
esac

# The plain entry would shadow the package entry under /usr/share/xsessions
if sudo uxsm entry -i --plain --from-table bspwm 2>/dev/null; then
    fail "uxsm entry --plain hid the package's bspwm.desktop without -f"
fi
[ ! -e "$dir/bspwm.desktop" ] || fail "uxsm entry --plain wrote bspwm.desktop"
ok "uxsm entry does not hide a package's entry without -f"
