package sessionentry

import "strings"

// startsOwnAutostart dice si alguno de los nombres del escritorio es de una
// sesión conocida que lanza ella misma las entradas de autostart XDG, y cuál.
//
// Sirve para una sola cosa: que las entradas que genera uxsm lleven
// `--no-autostart` en esas sesiones. Si lo lanzaran las dos, cada entrada
// arrancaría dos veces; se comprobó con Xfce, donde ni xfce4-session ni systemd
// miran si el otro ya la ha lanzado.
//
// De una sesión que la tabla no conoce, uxsm no supone nada: la entrada sale
// sin la opción y el autostart lo lanza uxsm, que es lo que se espera de una
// sesión con systemd y lo que hace uwsm. Quien tenga una que lance el suyo y
// vea las entradas duplicadas, añade la opción.
func startsOwnAutostart(names []string) (string, bool) {
	for _, name := range names {
		if ownAutostart[strings.ToLower(name)] {
			return name, true
		}
	}
	return "", false
}

// ownAutostart dice, por cada nombre de escritorio conocido en minúsculas, si
// esa sesión lanza ella misma el autostart XDG.
var ownAutostart = func() map[string]bool {
	m := make(map[string]bool)
	for _, k := range known {
		for _, name := range k.DesktopNames {
			// Un nombre puede salir en varias entradas ―"GNOME" en GNOME, en
			// Budgie y en openbox-gnome―; basta con que una lo lance.
			m[strings.ToLower(name)] = m[strings.ToLower(name)] || k.OwnAutostart
		}
	}
	return m
}()
