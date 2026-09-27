// Package sessionenv prepares the session environment in the systemd manager
// and restores it when the session ends. It is the uxsm equivalent of
// `uwsm aux prepare-env` and `uwsm aux cleanup-env`, using the same rules.
package sessionenv

// set is a set of variable names.
type set map[string]bool

func newSet(names ...string) set {
	s := make(set, len(names))
	for _, n := range names {
		s[n] = true
	}
	return s
}

func (s set) union(other set) set {
	u := make(set, len(s)+len(other))
	for n := range s {
		u[n] = true
	}
	for n := range other {
		u[n] = true
	}
	return u
}

// Variable lists copied from uwsm's Varnames class with three X11-specific
// changes: in uwsm, DISPLAY belongs to XWayland and is set by the compositor at
// startup; on X11 it is set by the display manager before the session and
// arrives in the login environment.
var (
	// sessionSpecific variables belong to each logind session: they are never
	// imported and are removed both at startup and shutdown.
	sessionSpecific = newSet("XDG_SEAT", "XDG_SEAT_PATH", "XDG_SESSION_ID", "XDG_SESSION_PATH", "XDG_VTNR")

	// alwaysUnset variables are removed from the manager at startup and never
	// imported. uwsm includes DISPLAY; uxsm does not because the correct X11
	// value comes from login.
	alwaysUnset = newSet("WAYLAND_DISPLAY").union(sessionSpecific)

	// alwaysExport variables are imported even if the manager already has the
	// same value. uxsm adds DISPLAY and XAUTHORITY, which the desktop needs to
	// connect to the X server.
	alwaysExport = newSet("PATH", "XDG_CURRENT_DESKTOP", "XDG_MENU_PREFIX", "XDG_SESSION_DESKTOP",
		"XDG_SESSION_CLASS", "XDG_SESSION_TYPE", "DISPLAY", "XAUTHORITY")

	// neverExport variables are never imported: they belong to the shell or
	// process, not the session. uxsm adds variables systemd sets for every service
	// it starts (JOURNAL_STREAM, MANAGERPID…); they reach the login environment
	// when a unit rather than a display manager launches the session, as in tests.
	neverExport = newSet("PWD", "LS_COLORS", "INVOCATION_ID", "SHLVL", "SHELL", "TERM", "COLORTERM",
		"TERM_SESSION_TYPE", "NOTIFY_SOCKET",
		"JOURNAL_STREAM", "MANAGERPID", "MANAGERPIDFDID", "SYSTEMD_EXEC_PID",
		"MEMORY_PRESSURE_WATCH", "MEMORY_PRESSURE_WRITE").union(sessionSpecific)

	// alwaysCleanup variables are removed at shutdown even if the session did not
	// import them, except those already present in the snapshot. uxsm adds
	// XAUTHORITY.
	alwaysCleanup = newSet("DISPLAY", "LANG", "PATH", "WAYLAND_DISPLAY", "XCURSOR_SIZE", "XCURSOR_THEME",
		"XDG_CURRENT_DESKTOP", "XDG_MENU_PREFIX", "XDG_SESSION_DESKTOP", "XDG_SESSION_CLASS",
		"XDG_SESSION_TYPE", "NOTIFY_SOCKET", "XAUTHORITY").union(sessionSpecific)

	// neverCleanup variables are never removed: the SSH agent may remain in use
	// after the session.
	neverCleanup = newSet("SSH_AGENT_LAUNCHER", "SSH_AUTH_SOCK", "SSH_AGENT_PID")
)
