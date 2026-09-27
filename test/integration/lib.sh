# Shared integration-test functions, loaded with `. lib.sh`.

# ok and fail report each check; fail also terminates the test.
ok() { echo "  ok: $*"; }
fail() { echo "  FAIL: $*"; exit 1; }

# wait_for SECONDS COMMAND...: repeat the command every tenth of a second until
# it succeeds. Return an error on timeout.
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

# wait_stopped SECONDS UNITS...: wait until none is active, activating, or
# deactivating. `systemctl is-active` also fails for a deactivating unit even
# while its ExecStopPost= may still be running.
wait_stopped() {
    limit=$1
    shift
    wait_for "$limit" sh -c '! systemctl --user is-active "$@" | grep -qvx -e inactive -e failed' sh "$@"
}

# wait_no_session SECONDS: wait until no uxsm session unit remains live. A
# stopped desktop is insufficient: environment cleanup runs in uxsm-env@'s
# ExecStopPost=, and session files remain until it finishes.
wait_no_session() {
    wait_for "${1:-15}" sh -c 'test -z "$(systemctl --user list-units --state=active,activating,deactivating --no-legend "uxsm-desktop@*.service" "uxsm-env@*.service" "uxsm-session@*.target" "uxsm-bindpid@*.service" uxsm-shutdown.target)"'
}

# start_xvfb :N starts a headless X server on display :N as a transient
# systemd --user unit and waits for it to accept connections.
start_xvfb() {
    n=${1#:}
    # The name deliberately includes xvfb: shell function variables are global,
    # and tests already use "unit" for their own purposes.
    xvfb_unit=uxsm-it-xvfb-$n.service
    # The previous test stopped it moments ago, and systemd retains the name
    # until fully released; systemd-run would refuse to reuse it before then.
    systemctl --user stop "$xvfb_unit" 2>/dev/null || true
    systemctl --user reset-failed "$xvfb_unit" 2>/dev/null || true
    wait_for 15 sh -c "[ \"\$(systemctl --user show -p LoadState --value $xvfb_unit)\" = not-found ]" ||
        fail "$xvfb_unit is still known to systemd: $(systemctl --user show -p LoadState -p ActiveState --value "$xvfb_unit" | tr '\n' ' ')"
    systemd-run --user --quiet --collect --unit="uxsm-it-xvfb-$n" Xvfb ":$n" -screen 0 1280x800x24
    wait_for 10 test -S "/tmp/.X11-unix/X$n" || fail "Xvfb :$n did not start"
}

# make_test_entries creates two local session entries identical on every
# distribution under ~/.local/share/xsessions by creating uxsm-it-names.desktop with
# DesktopNames=TestDE;Second;, and uxsm-it-nonames.desktop without DesktopNames=.
# Both run bspwm. remove_test_entries deletes them.
make_test_entries() {
    entries=$HOME/.local/share/xsessions
    mkdir -p "$entries"
    cat >"$entries/uxsm-it-names.desktop" <<ENTRY
[Desktop Entry]
Type=Application
Name=uxsm test, with DesktopNames
Exec=bspwm
DesktopNames=TestDE;Second;
ENTRY
    cat >"$entries/uxsm-it-nonames.desktop" <<ENTRY
[Desktop Entry]
Type=Application
Name=uxsm test, without DesktopNames
Exec=bspwm
ENTRY
}

remove_test_entries() {
    rm -f "$HOME/.local/share/xsessions/uxsm-it-names.desktop" \
        "$HOME/.local/share/xsessions/uxsm-it-nonames.desktop"
}

# run_session ENTRY [VAR=value…] -- [ARGS…]: start ENTRY's session as the display
# manager would on :5, with those environment variables and uxsm start arguments,
# and wait for the complete session. Store the desktop unit name in $desktop.
#
# The session process is the transient uxsm-it-session unit. A systemd-run unit
# inherits the manager environment while a display manager does not, so uxsm
# start runs through `env -u XDG_CURRENT_DESKTOP` to avoid a stale manager value.
run_session() {
    entry=$1
    shift
    assignments=""
    while [ "$1" != "--" ]; do
        assignments="$assignments $1"
        shift
    done
    shift
    # shellcheck disable=SC2086
    systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 \
        env -u XDG_CURRENT_DESKTOP $assignments uxsm start "$@" "$entry"
    desktop=uxsm-desktop@$entry.service
    wait_for 15 systemctl --user is-active graphical-session.target ||
        fail "the session of $entry did not start with: $*"
}

# stop_session closes the session with uxsm stop and waits until nothing remains.
stop_session() {
    uxsm stop
    wait_stopped 15 uxsm-it-session.service "$desktop" ||
        fail "the session did not stop"
}

# expect_var NAME VALUE: the desktop process has that variable with that value.
# An empty VALUE means the variable is absent or empty.
expect_var() {
    pid=$(systemctl --user show -p MainPID --value "$desktop")
    got=$(tr '\0' '\n' <"/proc/$pid/environ" | sed -n "s/^$1=//p")
    [ "$got" = "$2" ] || fail "$1 is '$got', expected '$2'"
}
