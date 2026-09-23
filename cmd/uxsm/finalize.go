package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/heizeisaburou/uxsm/internal/session"
)

// runFinalize enciende la señal de que la sesión está lista: `uxsm finalize`.
//
// Es lo mismo que hace uwsm con su `uwsm finalize`: lo ejecuta el escritorio
// desde su configuración ―una línea en bspwmrc, en autostart de Xfce o donde
// sea― cuando se considera arrancado. En X11 uxsm no lo necesita, porque lo ve
// él solo en cuanto el gestor de ventanas deja su marca de EWMH, pero un
// escritorio que no ponga esa marca, o que quiera decirlo más tarde, tiene así
// cómo decirlo.
//
// Los dos caminos encienden la misma señal y sólo cuenta el primero: si la
// sesión ya estaba lista, esto no es un error ni vuelve a arrancar nada; lo
// dice y termina bien, para que un escritorio que lo llame de más no se rompa.
func runFinalize(args []string) error {
	fs := newFlagSet("finalize", "",
		"Tell uxsm that the desktop of the session is up, from the desktop\n"+
			"itself. uxsm also notices it on its own when a window manager takes\n"+
			"over the X display, and only the first of the two counts.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return errUsage
	}

	dir, err := session.RuntimeDir()
	if err != nil {
		return err
	}
	// La identidad la escribe uxsm start y la borra el cierre de la sesión, así
	// que es lo que dice si hay una sesión de uxsm ahora mismo.
	if _, err := os.Stat(filepath.Join(dir, session.IdentityFile)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("there is no uxsm session to finalize: run uxsm finalize from the " +
				"desktop of a session started by uxsm start")
		}
		return err
	}

	first, err := session.SignalReady("the desktop ran uxsm finalize")
	if err != nil {
		return err
	}
	if !first {
		_, reason, err := session.Ready()
		if err != nil {
			return err
		}
		fmt.Printf("The session was already ready (%s); nothing to do.\n", reason)
		return nil
	}
	fmt.Println("The session is ready.")
	return nil
}
