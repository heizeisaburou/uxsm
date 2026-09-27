#!/bin/sh
# Step 2: the session activates graphical-session.target, watches the session
# process, and shuts down completely regardless of the trigger: desktop exit,
# termination of the session process as a display manager would do, or
# `uxsm stop`.

set -eu
. "$(dirname "$0")/lib.sh"

desktop=uxsm-desktop@bspwm.desktop.service
session=uxsm-session@bspwm.desktop.target

cleanup() {
    systemctl --user stop uxsm-it-session.service uxsm-it-xvfb-5.service 2>/dev/null || true
}
trap cleanup EXIT

# start_session starts uxsm as the display manager would and verifies the whole
# session: desktop, session target, graphical-session.target, and the session
# process PID watcher.
start_session() {
    systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 uxsm start bspwm.desktop
    wait_for 15 systemctl --user is-active graphical-session.target ||
        fail "graphical-session.target did not become active"
    systemctl --user is-active --quiet "$desktop" "$session" || fail "$desktop or $session is not active"

    pid=$(systemctl --user show -p MainPID --value uxsm-it-session.service)
    systemctl --user is-active --quiet "uxsm-bindpid@$pid.service" ||
        fail "uxsm-bindpid@$pid.service is not watching the session process"
}

# expect_down HOW verifies that nothing remains after closing the session that way.
expect_down() {
    units="$desktop $session graphical-session.target uxsm-it-session.service"
    # shellcheck disable=SC2086
    wait_stopped 15 $units ||
        fail "after $1 still active: $(systemctl --user is-active $units | tr '\n' ' ')"
    wait_for 5 sh -c '! pgrep -u "$(id -u)" -x bspwm' || fail "after $1 bspwm is still running"
    [ -z "$(systemctl --user list-units --plain --no-legend --state=active 'uxsm-bindpid@*')" ] ||
        fail "after $1 a uxsm-bindpid unit is still active"
    ok "$1 shuts the whole session down"
}

start_xvfb :5

start_session
ok "the session activates graphical-session.target and watches its process"

DISPLAY=:5 bspc quit
expect_down "quitting bspwm"

start_session
systemctl --user kill --signal=TERM uxsm-it-session.service
expect_down "killing the session process"

start_session
uxsm stop
expect_down "uxsm stop"
