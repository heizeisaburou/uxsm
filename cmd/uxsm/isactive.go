package main

import (
	"fmt"

	"github.com/heizeisaburou/uxsm/internal/systemd"
)

// runIsActive dice si hay una sesión de uxsm en marcha: `uxsm check is-active`.
//
// Se llama igual que en uwsm y sirve para lo mismo: que un script sepa dónde
// está. Lo natural para un fichero de arranque del escritorio, o para algo que
// se lance desde fuera y quiera saber si puede usar `uxsm app`.
//
// A diferencia del informe de `uxsm check`, ésta contesta con el código de
// salida y no escribe nada, salvo que se le pida con -v.
func runIsActive(args []string) error {
	fs := newFlagSet("check is-active", "",
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
