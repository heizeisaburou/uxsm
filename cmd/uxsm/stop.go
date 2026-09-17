package main

import "github.com/heizeisaburou/uxsm/internal/systemd"

// runStop apaga la sesión desde fuera: `uxsm stop`.
//
// Arranca uxsm-shutdown.target, el mismo target que usan el escritorio y bindpid
// cuando terminan. systemd para todo lo que choca con él, así que la sesión se
// cierra por el mismo camino venga de donde venga el cierre.
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
