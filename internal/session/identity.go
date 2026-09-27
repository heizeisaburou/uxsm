package session

import "strings"

// IdentityVars returns the variables that identify the session, derived from
// the desktop names and formatted as "NAME=value".
//
// These are the same variables uwsm sets, with a different session type:
//   - XDG_CURRENT_DESKTOP: all names, separated by ":".
//   - XDG_SESSION_DESKTOP: the first name.
//   - XDG_MENU_PREFIX: the lowercase first name followed by "-", the prefix
//     used by XDG menu files ("xfce-applications.menu").
//   - XDG_SESSION_TYPE: always "x11".
//
// names cannot be empty: DesktopNames never returns an empty result without an error.
func IdentityVars(names []string) []string {
	first := names[0]
	return []string{
		"XDG_CURRENT_DESKTOP=" + strings.Join(names, ":"),
		"XDG_SESSION_DESKTOP=" + first,
		"XDG_MENU_PREFIX=" + strings.ToLower(first) + "-",
		"XDG_SESSION_TYPE=x11",
	}
}
