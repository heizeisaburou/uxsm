package main

import (
	"fmt"

	"github.com/heizeisaburou/uxsm/internal/systemd"
)

// runIsActive dice si hay una sesión de uxsm en marcha: `uxsm is-active`.
//
// Es lo mismo que el `uwsm check is-active` de uwsm, y sirve para lo mismo: que
// un script sepa dónde está. Lo natural para un fichero de arranque del
// escritorio o para algo que se lance desde fuera y quiera saber si puede usar
// `uxsm app`.
//
// No va dentro de `uxsm check` porque aquélla es otra cosa: comprueba si el
// sistema está listo para uxsm, ejecuta todas sus comprobaciones y escribe un
// informe. Ésta contesta una pregunta con el código de salida y, por eso
// mismo, no escribe nada salvo que se le pida con -v.
func runIsActive(args []string) error {
	fs := newFlagSet("is-active", "",
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
