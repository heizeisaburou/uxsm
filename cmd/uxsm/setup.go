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
		{"xsessions-dir", "make the display manager read " + dm.LocalXSessions, runSetupXSessionsDir, false},
	},
}

func runSetup(args []string) error {
	return setupCommands.dispatch(args)
}

// runSetupXSessionsDir hace que el display manager lea dm.LocalXSessions, donde
// uxsm entry instala las entradas: `uxsm setup xsessions-dir [-i] [lightdm|sddm|gdm]`.
//
// Sin display manager, usa el que está en uso. Sin -i sólo explica qué haría.
// Con GDM nunca escribe nada: su lista sale de su XDG_DATA_DIRS, que también
// decide dónde busca todo lo demás, y eso es mejor que lo cambie una persona.
func runSetupXSessionsDir(args []string) error {
	fs := newFlagSet("setup xsessions-dir", "[-i] [lightdm | sddm | gdm]",
		"Make the display manager read "+dm.LocalXSessions+", where uxsm installs the\n"+
			"session entries it generates, so that they show on the login screen.\n"+
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

	fmt.Printf("%s reads session entries from: %s\n  from: %s\n", r.Name, strings.Join(r.Dirs, ", "), r.Origin)
	if r.ID == "gdm" {
		explainGDM(r)
		return nil
	}

	c, err := dm.SetupXSessionsDir(r.ID)
	if err != nil {
		return err
	}
	if c == nil {
		fmt.Printf("It already reads %s: nothing to do.\n", dm.LocalXSessions)
		return nil
	}

	action := "change"
	if c.Old == "" {
		action = "create"
	}
	if *install {
		fmt.Printf("\nGoing to %s %s:\n", action, c.File)
	} else {
		fmt.Printf("\nWould %s %s:\n", action, c.File)
	}
	printChange(c)

	if !*install {
		fmt.Printf("\nRun it again with -i to do it (as root). %s reads it when it starts.\n", r.Name)
		return nil
	}
	if err := c.Apply(); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("writing %s: %w (run it as root)", c.File, err)
		}
		return fmt.Errorf("writing %s: %w", c.File, err)
	}
	fmt.Printf("\nDone. %s will read %s the next time it starts.\n", r.Name, dm.LocalXSessions)
	return nil
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
	if r.Reads(dm.LocalXSessions) {
		fmt.Printf("It already reads %s: nothing to do.\n", dm.LocalXSessions)
		return
	}
	fmt.Printf(`
GDM looks for session entries in the xsessions subdirectory of every
directory in its XDG_DATA_DIRS, and in /usr/share/xsessions. To make it read
%s, add /usr/local/share to XDG_DATA_DIRS in its unit:

  systemctl edit %s

  [Service]
  Environment=XDG_DATA_DIRS=/usr/local/share:/usr/share

Keep every directory it already has: XDG_DATA_DIRS also tells GDM where to
find icons, schemas and the rest of its data, not only session entries.
uxsm does not change GDM's configuration, not even with -i.
`, dm.LocalXSessions, r.Unit)
}
