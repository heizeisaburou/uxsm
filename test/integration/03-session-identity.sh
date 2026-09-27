#!/bin/sh
# Step 3a: uxsm start computes the session identity with -D and -e, stores it in
# $XDG_RUNTIME_DIR/uxsm alongside the login environment, and passes it to the
# desktop environment.
#
# Use entries from make_test_entries because distributions package bspwm.desktop
# differently: Ubuntu 24.04 (bspwm 0.9.10-2) omits DesktopNames=, while Arch
# includes it.

set -eu
. "$(dirname "$0")/lib.sh"

make_test_entries

cleanup() {
    systemctl --user stop uxsm-it-session.service uxsm-it-xvfb-5.service 2>/dev/null || true
    remove_test_entries
}
trap cleanup EXIT

start_xvfb :5

# Without -e: DesktopNames= from the entry, then -D.
run_session uxsm-it-names.desktop -- -D Extra
expect_var XDG_CURRENT_DESKTOP TestDE:Second:Extra
expect_var XDG_SESSION_DESKTOP TestDE
expect_var XDG_MENU_PREFIX testde-
expect_var XDG_SESSION_TYPE x11
expect_var DISPLAY :5
ok "without -e, the desktop gets the entry names, then -D, and the XDG_* identity"

dir=$XDG_RUNTIME_DIR/uxsm
tr '\0' '\n' <"$dir/env_login" | grep -qx 'DISPLAY=:5' || fail "env_login does not have DISPLAY=:5"
tr '\0' '\n' <"$dir/env_identity" | grep -qx 'XDG_CURRENT_DESKTOP=TestDE:Second:Extra' ||
    fail "env_identity does not have the computed XDG_CURRENT_DESKTOP"
ok "the login environment and the identity are saved in \$XDG_RUNTIME_DIR/uxsm"
stop_session

# Without -e, an existing XDG_CURRENT_DESKTOP comes first.
run_session uxsm-it-names.desktop XDG_CURRENT_DESKTOP=Inherited --
expect_var XDG_CURRENT_DESKTOP Inherited:TestDE:Second
ok "without -e, an existing XDG_CURRENT_DESKTOP goes first"
stop_session

# With -e: only -D, even with an inherited XDG_CURRENT_DESKTOP.
run_session uxsm-it-names.desktop XDG_CURRENT_DESKTOP=Inherited -- -e -D Only
expect_var XDG_CURRENT_DESKTOP Only
expect_var XDG_SESSION_DESKTOP Only
ok "with -e, only the -D names are used"
stop_session

# An entry without DesktopNames= or another source: use the executable name.
run_session uxsm-it-nonames.desktop --
expect_var XDG_CURRENT_DESKTOP bspwm
ok "an entry without DesktopNames= falls back to the executable name"
stop_session

# -e without -D is an argument error detected before touching systemd.
status=0
uxsm start -e uxsm-it-names.desktop 2>/dev/null || status=$?
[ "$status" = 2 ] || fail "uxsm start -e without -D exited with $status, expected 2"
ok "-e without -D is a usage error"
