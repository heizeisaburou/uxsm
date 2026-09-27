package main

import (
	"fmt"

	"github.com/heizeisaburou/uxsm/internal/systemd"
)

// runIsActive says whether a uxsm session is running: `uxsm check is-active`.
//
// It has the same name and purpose as in uwsm: letting a script know where it
// is running. This is useful in a desktop startup file, or for something
// launched from outside that needs to know whether it can use `uxsm app`.
//
// Unlike the `uxsm check` report, it answers through the exit status and writes
// nothing unless -v is requested.
func runIsActive(args []string) error {
	fs := newFlagSet("check is-active", "[-v]",
		"Exit with 0 if a uxsm session is running or starting, and with 1 if not.")
	verbose := fs.Bool("v", false, "write the units that are up")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return errUsage
	}

	live, err := systemd.LiveUnits(uxsmUnits...)
	if err != nil {
		return err
	}
	if *verbose {
		for _, unit := range live {
			fmt.Println(unit)
		}
	}
	if len(live) == 0 {
		return errNo
	}
	return nil
}
