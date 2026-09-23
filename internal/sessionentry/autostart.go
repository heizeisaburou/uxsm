package sessionentry

import "strings"

// startsOwnAutostart dice si alguno de los nombres del escritorio es de un
// escritorio conocido que lanza él mismo las entradas de autostart XDG, y cuál.
//
// Sirve para una sola cosa: que las entradas que genera uxsm lleven
// `--no-autostart` en esos escritorios. Si lo lanzaran los dos, cada entrada
// arrancaría dos veces; se comprobó con Xfce, donde ni xfce4-session ni systemd
// miran si el otro ya la ha lanzado.
//
// De un escritorio que la tabla no conoce, uxsm no supone nada: la entrada sale
// sin la opción y el autostart se lanza, que es lo que se espera de una sesión
// con systemd y lo que hace uwsm. Quien tenga uno que lance el suyo y vea las
// entradas duplicadas, añade la opción.
func startsOwnAutostart(names []string) (string, bool) {
	for _, name := range names {
		if wm, ok := desktopIsWM[strings.ToLower(name)]; ok && !wm {
			return name, true
		}
	}
	return "", false
}

// desktopIsWM dice, por cada nombre de escritorio conocido en minúsculas, si es
// un gestor de ventanas suelto. Los nombres de los gestores de ventanas son el
// nombre de su programa, así que no chocan con los de los escritorios.
var desktopIsWM = func() map[string]bool {
	m := make(map[string]bool)
	for _, k := range known {
		for _, name := range k.DesktopNames {
			m[strings.ToLower(name)] = k.WindowManager
		}
	}
	return m
}()
