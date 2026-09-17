package main

import (
	"fmt"

	"github.com/heizeisaburou/uxsm/internal/desktopentry"
	"github.com/heizeisaburou/uxsm/internal/systemd"
)

// runStart arranca la sesión de una entrada: `uxsm start bspwm.desktop`.
//
// Es lo que ejecuta el display manager, así que el proceso que vigila es éste:
//  1. Busca la entrada en los directorios xsessions, para fallar ya si no existe
//     o no tiene Exec=, antes de tocar systemd.
//  2. Copia DISPLAY y XAUTHORITY al gestor de systemd: el escritorio va a correr
//     como servicio, y los servicios sólo ven el entorno del gestor.
//  3. Se sustituye por `systemctl --user start --wait uxsm-desktop@<id>.service`,
//     que no vuelve hasta que el escritorio termina.
func runStart(args []string) error {
	fs := newFlagSet("start", "<entry.desktop>",
		"Start the X11 session described by a session entry from the xsessions\n"+
			"directories, running its Exec= as a systemd user service.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}
	id := fs.Arg(0)

	if _, err := desktopentry.Find("xsessions", id); err != nil {
		return err
	}
	if err := systemd.CheckInstance(id); err != nil {
		return err
	}

	if err := systemd.ImportEnvironment("DISPLAY", "XAUTHORITY"); err != nil {
		return fmt.Errorf("importing the display into systemd: %w", err)
	}
	return systemd.ExecStartWait(systemd.DesktopUnit(id))
}
