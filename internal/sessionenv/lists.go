// Package sessionenv prepara el entorno de la sesión en el gestor de systemd y lo
// deja como estaba al cerrarla. Es la parte de uxsm que hace lo que uwsm hace en
// `uwsm aux prepare-env` y `uwsm aux cleanup-env`, con el mismo criterio.
package sessionenv

// set es un conjunto de nombres de variable.
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

// Listas de variables copiadas de la clase Varnames de uwsm, con tres cambios
// para X11: en uwsm, DISPLAY es el de XWayland y lo pone el compositor al
// arrancar; en X11 lo pone el display manager antes de la sesión y llega en el
// entorno de login.
var (
	// sessionSpecific son propias de cada sesión de logind: no se suben nunca, y
	// se borran al empezar y al cerrar.
	sessionSpecific = newSet("XDG_SEAT", "XDG_SEAT_PATH", "XDG_SESSION_ID", "XDG_SESSION_PATH", "XDG_VTNR")

	// alwaysUnset se borran del gestor al empezar y no se suben. uwsm incluye
	// DISPLAY; uxsm no, porque en X11 el bueno viene del login.
	alwaysUnset = newSet("WAYLAND_DISPLAY").union(sessionSpecific)

	// alwaysExport se suben aunque el gestor ya tuviera el mismo valor. uxsm
	// añade DISPLAY y XAUTHORITY, las que necesita el escritorio para conectarse
	// al servidor X.
	alwaysExport = newSet("PATH", "XDG_CURRENT_DESKTOP", "XDG_MENU_PREFIX", "XDG_SESSION_DESKTOP",
		"XDG_SESSION_CLASS", "XDG_SESSION_TYPE", "DISPLAY", "XAUTHORITY")

	// neverExport no se suben nunca: son de la shell o del proceso, no de la
	// sesión. uxsm añade las que systemd pone a cada servicio que arranca
	// (JOURNAL_STREAM, MANAGERPID…): llegan al entorno de login si la sesión la
	// lanza una unidad en vez de un display manager, como en las pruebas.
	neverExport = newSet("PWD", "LS_COLORS", "INVOCATION_ID", "SHLVL", "SHELL", "TERM", "COLORTERM",
		"TERM_SESSION_TYPE", "NOTIFY_SOCKET",
		"JOURNAL_STREAM", "MANAGERPID", "MANAGERPIDFDID", "SYSTEMD_EXEC_PID",
		"MEMORY_PRESSURE_WATCH", "MEMORY_PRESSURE_WRITE").union(sessionSpecific)

	// alwaysCleanup se borran al cerrar aunque no las haya subido la sesión,
	// salvo las que ya estaban en la foto. uxsm añade XAUTHORITY.
	alwaysCleanup = newSet("DISPLAY", "LANG", "PATH", "WAYLAND_DISPLAY", "XCURSOR_SIZE", "XCURSOR_THEME",
		"XDG_CURRENT_DESKTOP", "XDG_MENU_PREFIX", "XDG_SESSION_DESKTOP", "XDG_SESSION_CLASS",
		"XDG_SESSION_TYPE", "NOTIFY_SOCKET", "XAUTHORITY").union(sessionSpecific)

	// neverCleanup no se borran nunca: el agente de SSH puede seguir en uso
	// después de la sesión.
	neverCleanup = newSet("SSH_AGENT_LAUNCHER", "SSH_AUTH_SOCK", "SSH_AGENT_PID")
)
