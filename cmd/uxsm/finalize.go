package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/heizeisaburou/uxsm/internal/session"
)

// runFinalize turns on the signal that says the session is ready: `uxsm finalize`.
//
// It is equivalent to uwsm's `uwsm finalize`: the desktop runs it from its
// configuration—a line in bspwmrc, Xfce autostart, or elsewhere—when it
// considers itself started. uxsm does not need this on X11 because it detects
// the EWMH marker as soon as the window manager publishes it, but this gives a
// desktop that does not publish the marker, or wants to declare readiness later,
// a way to do so.
//
// Both paths turn on the same signal and only the first one counts: if the
// session was already ready, this is not an error and does not restart anything;
// it reports that state and succeeds so an extra desktop call does not break.
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
	// uxsm start writes the identity and session shutdown removes it, so its
	// presence proves that a uxsm session is currently running.
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
