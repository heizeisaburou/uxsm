package session

import "strings"

// IdentityVars son las variables que dicen qué sesión es, calculadas a partir
// de los nombres del escritorio, en formato "NOMBRE=valor".
//
// Son las mismas que pone uwsm, cambiando el tipo de sesión:
//   - XDG_CURRENT_DESKTOP: todos los nombres, separados por ":".
//   - XDG_SESSION_DESKTOP: el primero.
//   - XDG_MENU_PREFIX: el primero en minúsculas y con "-" detrás, el prefijo
//     de los ficheros de menú de XDG ("xfce-applications.menu").
//   - XDG_SESSION_TYPE: siempre "x11".
//
// names no puede estar vacío: DesktopNames nunca lo devuelve vacío sin error.
func IdentityVars(names []string) []string {
	first := names[0]
	return []string{
		"XDG_CURRENT_DESKTOP=" + strings.Join(names, ":"),
		"XDG_SESSION_DESKTOP=" + first,
		"XDG_MENU_PREFIX=" + strings.ToLower(first) + "-",
		"XDG_SESSION_TYPE=x11",
	}
}
