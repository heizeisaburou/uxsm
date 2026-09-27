package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/heizeisaburou/uxsm/internal/dm"
)

// setupCommands are the fixes provided by uxsm setup: each is a named fix for
// one specific issue rather than an arbitrary script.
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

// runSetupSessionsDir makes the display manager read the local session
// directories: `uxsm setup sessions-dir [-i] [lightdm|sddm|gdm]`.
//
// There are two, for X11 and Wayland. uxsm generates only X11 entries, but
// fixing only that directory would leave the machine half-configured: LightDM
// uses one list for both, and a hand-written Wayland entry—as recommended by
// uwsm's README for its compositor—would remain hidden.
//
// Without an explicit display manager, it uses the active one. Without -i it
// only explains the change. It never writes GDM configuration: its list comes
// from XDG_DATA_DIRS, which controls all other data lookup too and is better
// changed by a person.
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
		// Do not put a colon after the path: this lets users copy it from the
		// terminal with a double click without capturing punctuation.
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

// explainRestart says that the display manager must be restarted and how, but
// does not restart it: that would terminate the graphical session running this
// command, and the person at the machine must make that decision.
//
// This is not cosmetic: the greeter reads directories independently, so it can
// offer the new session before the display manager knows how to start it. When
// selected, the greeter closes, startup fails, and the seat remains black.
func explainRestart(r *dm.Report) {
	unit := r.Unit
	if unit == "" {
		unit = "display-manager.service"
	}
	fmt.Printf(`
The running %s has not reloaded its session directories. Restart it, or the
machine, before logging out. Until then its login screen can offer the new
session but fail to launch it, leaving the screen black. From another console
(Ctrl+Alt+F2), run

  sudo systemctl restart %s

uxsm does not restart it because that would end your current graphical session.
`, r.Name, unit)
}

// missingOr returns the directories the display manager lacked, for the final
// message; if it already read all of them, it returns both.
func missingOr(r *dm.Report) []string {
	if missing := r.Missing(); len(missing) > 0 {
		return missing
	}
	return dm.LocalSessions
}

// printChange displays the changed line when there is exactly one, otherwise
// the complete new file.
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

// explainGDM explains how GDM chooses its directories and how to change them,
// without modifying anything.
func explainGDM(r *dm.Report) {
	if len(r.Missing()) == 0 {
		fmt.Printf("It already reads them, nothing to do\n")
		return
	}
	fmt.Printf(`
GDM looks for session entries in the xsessions subdirectory of every
directory in its XDG_DATA_DIRS, and in the /usr/share/xsessions directory. To make it read
%s, add /usr/local/share to XDG_DATA_DIRS in its unit with

  systemctl edit %s

  [Service]
  Environment=XDG_DATA_DIRS=/usr/local/share:/usr/share

Keep every directory it already has: XDG_DATA_DIRS also tells GDM where to
find icons, schemas and the rest of its data, not only session entries.
uxsm does not change GDM's configuration, not even with -i.
`, dm.LocalXSessions, r.Unit)
}
