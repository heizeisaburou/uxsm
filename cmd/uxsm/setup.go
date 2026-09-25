package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/heizeisaburou/uxsm/internal/dm"
)

// setupCommands son los arreglos de uxsm setup: cada uno arregla una cosa
// concreta, con su nombre, y no un script cualquiera.
var setupCommands = group{
	name:        "uxsm setup",
	description: "Change the system so that uxsm works fully. Without -i, each command\nonly explains what it would do.",
	commands: []command{
		{"sessions-dir", "make the display manager read the local session directories", runSetupSessionsDir, false},
	},
}

func runSetup(args []string) error {
	return setupCommands.dispatch(args)
}

// runSetupSessionsDir hace que el display manager lea los directorios locales
// de sesiones: `uxsm setup sessions-dir [-i] [lightdm|sddm|gdm]`.
//
// Son dos, el de X11 y el de Wayland. uxsm sólo genera entradas de X11, pero
// arreglar sólo ese sería dejar la máquina a medias: en LightDM es la misma
// lista para los dos, y quien escriba a mano una entrada de Wayland ―como pide
// el README de uwsm para su compositor― se encontraría con que no sale.
//
// Sin display manager, usa el que está en uso. Sin -i sólo explica qué haría.
// Con GDM nunca escribe nada: su lista sale de su XDG_DATA_DIRS, que también
// decide dónde busca todo lo demás, y eso es mejor que lo cambie una persona.
func runSetupSessionsDir(args []string) error {
	fs := newFlagSet("setup sessions-dir", "[-i] [lightdm | sddm | gdm]",
		"Make the display manager read "+strings.Join(dm.LocalSessions, " and ")+",\n"+
			"where session entries installed by hand live, the ones uxsm generates\n"+
			"among them, so that they show on the login screen.\n"+
			"Without a display manager, it uses the one in use. Without -i, it only\n"+
			"explains what it would change. For GDM it never changes anything: it only\n"+
			"explains how to do it.")
	install := fs.Bool("i", false, "make the change (needs root)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return errUsage
	}

	var r *dm.Report
	var err error
	if fs.NArg() == 1 {
		r, err = dm.Get(fs.Arg(0))
	} else {
		r, err = dm.Active()
	}
	if err != nil {
		return err
	}

	fmt.Printf("%s reads session entries from\n  %s\nas set in\n  %s\n",
		r.Name, strings.Join(r.Dirs, ", "), r.Origin)
	if r.ID == "gdm" {
		explainGDM(r)
		return nil
	}

	changes, err := dm.SetupSessionsDir(r.ID)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		fmt.Printf("It already reads them, nothing to do\n")
		return nil
	}

	for i := range changes {
		c := &changes[i]
		action := "change"
		if c.Old == "" {
			action = "create"
		}
		// Sin dos puntos detrás de la ruta: así se copia de la terminal con
		// dos clics, sin arrastrar el signo.
		if *install {
			fmt.Printf("\nGoing to %s this file\n  %s\n", action, c.File)
		} else {
			fmt.Printf("\nWould %s this file\n  %s\n", action, c.File)
		}
		printChange(c)
	}

	if !*install {
		fmt.Printf("\nRun it again with -i to do it (as root). %s reads it when it starts.\n", r.Name)
		return nil
	}
	for i := range changes {
		c := &changes[i]
		if err := c.Apply(); err != nil {
			if errors.Is(err, os.ErrPermission) {
				return fmt.Errorf("writing %s: %w (run it as root)", c.File, err)
			}
			return fmt.Errorf("writing %s: %w", c.File, err)
		}
	}
	fmt.Printf("\nDone. %s will read %s the next time it starts.\n",
		r.Name, strings.Join(missingOr(r), " and "))
	explainRestart(r)
	return nil
}

// explainRestart dice que hay que reiniciar el display manager y cómo, sin
// hacerlo: reiniciarlo se lleva por delante la sesión gráfica desde la que se
// está ejecutando esto, y eso lo decide quien está delante, no uxsm.
//
// No es un detalle: su greeter lee los directorios por su cuenta, así que
// ofrece la sesión nueva antes de que el display manager sepa arrancarla, y al
// elegirla cierra el greeter, falla, y deja el asiento en negro.
func explainRestart(r *dm.Report) {
	unit := r.Unit
	if unit == "" {
		unit = "display-manager.service"
	}
	fmt.Printf(`
The %s that is running still does not know them: restart it, or the machine,
before logging out. Until then its login screen can offer the session and then
fail to launch it, leaving the screen black. From another console (Ctrl+Alt+F2)

  sudo systemctl restart %s

uxsm does not do it: it would take down the graphical session you are in.
`, r.Name, unit)
}

// missingOr son los directorios que le faltaban al display manager, para
// decirlo al terminar; si ya los leía todos, los dos.
func missingOr(r *dm.Report) []string {
	if missing := r.Missing(); len(missing) > 0 {
		return missing
	}
	return dm.LocalSessions
}

// printChange enseña un cambio: la línea que cambia, si sólo cambia una, o el
// fichero nuevo entero.
func printChange(c *dm.Change) {
	oldLines, newLines := strings.Split(c.Old, "\n"), strings.Split(c.New, "\n")
	if c.Old != "" && len(oldLines) == len(newLines) {
		var diff []int
		for i := range oldLines {
			if oldLines[i] != newLines[i] {
				diff = append(diff, i)
			}
		}
		if len(diff) == 1 {
			i := diff[0]
			fmt.Printf("  line %d\n  - %s\n  + %s\n", i+1, oldLines[i], newLines[i])
			return
		}
	}
	for _, l := range strings.Split(strings.TrimSuffix(c.New, "\n"), "\n") {
		fmt.Printf("  %s\n", l)
	}
}

// explainGDM explica cómo decide GDM sus directorios y cómo cambiarlos, sin
// cambiar nada.
func explainGDM(r *dm.Report) {
	if len(r.Missing()) == 0 {
		fmt.Printf("It already reads them, nothing to do\n")
		return
	}
	fmt.Printf(`
GDM looks for session entries in the xsessions subdirectory of every
directory in its XDG_DATA_DIRS, and in /usr/share/xsessions. To make it read
%s, add /usr/local/share to XDG_DATA_DIRS in its unit with

  systemctl edit %s

  [Service]
  Environment=XDG_DATA_DIRS=/usr/local/share:/usr/share

Keep every directory it already has: XDG_DATA_DIRS also tells GDM where to
find icons, schemas and the rest of its data, not only session entries.
uxsm does not change GDM's configuration, not even with -i.
`, dm.LocalXSessions, r.Unit)
}
