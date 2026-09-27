#!/bin/sh
# Step 4: the session is not ready until one of two signals arrives, and only the
# first counts: uxsm sees the EWMH window manager on screen, or the desktop runs
# `uxsm finalize`. Until then the desktop is not considered started, so
# graphical-session.target waits; if neither signal arrives, the session is not
# left half-started: TimeoutStartSec= fails the service and everything stops.

set -eu
. "$(dirname "$0")/lib.sh"

entries=$HOME/.local/share/xsessions
slow=$HOME/uxsm-it-slowwm
final=$HOME/uxsm-it-finalize
dropin=$HOME/.config/systemd/user/uxsm-desktop@.service.d
slow_unit=uxsm-desktop@uxsm-it-slowwm.desktop.service
final_unit=uxsm-desktop@uxsm-it-finalize.desktop.service
nowm=uxsm-desktop@sleep.service

# The standalone `uxsm aux wait-ready` calls below are not a session, so nothing
# clears the ready signal afterwards. In a real session, `uxsm start` clears it
# at startup and cleanup clears it at shutdown. Because an active signal is
# exactly what the wait looks for, clear it between calls so each starts fresh.
clear_ready() { rm -f "$XDG_RUNTIME_DIR/uxsm/ready"; }

cleanup() {
    systemctl --user stop uxsm-it-session.service "$slow_unit" "$final_unit" "$nowm" \
        uxsm-it-wm.service uxsm-it-xvfb-5.service 2>/dev/null || true
    clear_ready
    rm -f "$entries/uxsm-it-slowwm.desktop" "$entries/uxsm-it-finalize.desktop" \
        "$slow" "$final" "$dropin/uxsm-it-timeout.conf"
    rmdir "$dropin" 2>/dev/null || true
    systemctl --user daemon-reload
}
trap cleanup EXIT

# Earlier tests may still be shutting down their last session.
wait_no_session || fail "a session from an earlier test is still shutting down"

# Without a session, there is nothing to mark ready.
if err=$(uxsm finalize 2>&1); then
    fail "uxsm finalize outside a session did not fail"
fi
case $err in
*"no uxsm session"*) ok "uxsm finalize outside a session says there is none" ;;
*) fail "uxsm finalize outside a session: $err" ;;
esac

start_xvfb :5

# Without a window manager or desktop signal, the wait times out and reports it.
clear_ready
if err=$(DISPLAY=:5 uxsm aux wait-ready -timeout 2s 2>&1); then
    fail "uxsm aux wait-ready succeeded on a display with no window manager"
fi
case $err in
*"no EWMH window manager"*) ok "with neither of the two, the wait times out and says so" ;;
*) fail "uxsm aux wait-ready on a bare display: $err" ;;
esac

# With a window manager, report which one answered.
clear_ready
systemd-run --user --quiet --collect --unit=uxsm-it-wm -E DISPLAY=:5 bspwm
got=$(DISPLAY=:5 uxsm aux wait-ready -timeout 10s)
case $got in
*"window manager ready: bspwm"*) ok "with a window manager, the wait reports its name" ;;
*) fail "uxsm aux wait-ready with bspwm printed: $got" ;;
esac

# After the window manager exits, any marker left on the root does not count:
# the window referenced by the marker no longer exists.
systemctl --user stop uxsm-it-wm.service
clear_ready
if err=$(DISPLAY=:5 uxsm aux wait-ready -timeout 2s 2>&1); then
    fail "uxsm aux wait-ready succeeded after the window manager was gone"
fi
ok "the mark of a window manager that is gone does not count"

# A desktop that takes time to start its window manager: without the wait,
# graphical-session.target would be active for these two seconds before there
# was anywhere to place windows.
cat >"$slow" <<'DESKTOP'
#!/bin/sh
sleep 2
exec bspwm
DESKTOP
chmod 755 "$slow"
mkdir -p "$entries"
cat >"$entries/uxsm-it-slowwm.desktop" <<ENTRY
[Desktop Entry]
Type=Application
Name=uxsm test, slow window manager
Exec=$slow
ENTRY

began=$(date +%s)
systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 \
    uxsm start uxsm-it-slowwm.desktop
wait_for 30 systemctl --user is-active graphical-session.target ||
    fail "the session of the slow desktop did not start"
elapsed=$(( $(date +%s) - began ))
[ "$elapsed" -ge 2 ] ||
    fail "the session was ready in ${elapsed}s, before its window manager was up"
DISPLAY=:5 xprop -root _NET_SUPPORTING_WM_CHECK 2>/dev/null | grep -q "window id" ||
    fail "graphical-session.target was reached before the window manager was up"
ok "graphical-session.target waits for the window manager"

DISPLAY=:5 bspc quit
wait_stopped 15 uxsm-it-session.service "$slow_unit" || fail "the session did not stop"

# The other path: a desktop without an EWMH window manager signals readiness itself.
cat >"$final" <<'DESKTOP'
#!/bin/sh
uxsm finalize
exec sleep infinity
DESKTOP
chmod 755 "$final"
cat >"$entries/uxsm-it-finalize.desktop" <<ENTRY
[Desktop Entry]
Type=Application
Name=uxsm test, finalize
Exec=$final
ENTRY

systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 \
    uxsm start uxsm-it-finalize.desktop
wait_for 20 systemctl --user is-active graphical-session.target ||
    fail "the session of a desktop that runs uxsm finalize did not become ready"
ok "a desktop with no window manager becomes ready by running uxsm finalize"

out=$(uxsm finalize) || fail "a second uxsm finalize failed: $out"
case $out in
*"already ready"*) ok "and the signal is only turned on once" ;;
*) fail "a second uxsm finalize said: $out" ;;
esac

uxsm stop
wait_stopped 15 uxsm-it-session.service "$final_unit" || fail "the finalized session did not stop"

# No window manager or signal: the session is not left half-started. A drop-in
# shortens the unit timeout; the same mechanism lets users remove the wait when
# launching something that is not a desktop.
mkdir -p "$dropin"
cat >"$dropin/uxsm-it-timeout.conf" <<CONF
[Service]
TimeoutStartSec=5
CONF
systemctl --user daemon-reload

systemd-run --user --quiet --collect --unit=uxsm-it-session -E DISPLAY=:5 uxsm start -- sleep infinity
sleep 2
if systemctl --user is-active -q graphical-session.target; then
    fail "the session became ready with neither a window manager nor uxsm finalize"
fi
ok "with neither of the two, the session never becomes ready"

wait_stopped 20 uxsm-it-session.service "$nowm" uxsm-env@sleep.service graphical-session.target ||
    fail "the session that was never ready did not stop when the wait timed out"
ok "and the whole session is shut down when the wait times out"
