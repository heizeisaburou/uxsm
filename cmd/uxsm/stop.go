package main

import "github.com/heizeisaburou/uxsm/internal/systemd"

// runStop shuts the session down externally: `uxsm stop`.
//
// It starts uxsm-shutdown.target, the same target used when the desktop or
// bindpid exits. systemd stops every unit that conflicts with it, so every
// source of shutdown follows the same path.
func runStop(args []string) error {
	fs := newFlagSet("stop", "", "Stop the running uxsm session.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return errUsage
	}
	return systemd.Start(systemd.ShutdownTarget)
}
