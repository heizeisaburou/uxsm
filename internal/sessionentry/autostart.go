package sessionentry

import (
	"fmt"
	"strings"
)

// Autostart dice si uxsm tiene que lanzar el autostart XDG de una sesión cuyo
// escritorio se llama names, y lo explica en una frase.
//
// Sólo lo lanza cuando la tabla dice que todos esos nombres son de gestores de
// ventanas sueltos. Un escritorio con gestor de sesión lanza sus entradas de
// autostart él mismo ―se comprobó con Xfce: ni xfce4-session ni systemd miran
// si el otro ya las ha lanzado, así que cada una arranca dos veces―, y de un
// escritorio que no conoce, uxsm no puede saber si lo hace.
//
// Entre no lanzar el autostart y lanzarlo dos veces, lo primero se ve enseguida
// y se arregla con `uxsm start -a yes`; lo segundo deja aplicaciones duplicadas
// y scripts ejecutados dos veces.
func Autostart(names []string) (bool, string) {
	for _, name := range names {
		wm, ok := desktopIsWM[strings.ToLower(name)]
		switch {
		case !ok:
			return false, fmt.Sprintf("uxsm does not know the desktop %q, so it does not start the XDG autostart: "+
				"the desktop may be starting it already", name)
		case !wm:
			return false, fmt.Sprintf("%s starts its own XDG autostart entries, so uxsm does not start them too", name)
		}
	}
	return true, fmt.Sprintf("%s is a window manager, so uxsm starts the XDG autostart of the session", names[0])
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
