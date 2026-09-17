package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/heizeisaburou/uxsm/internal/desktopentry"
)

// runAux agrupa las subórdenes internas, las que llaman las unidades de
// systemd de uxsm desde sus Exec*=. No salen en la ayuda general.
func runAux(args []string) error {
	const auxUsage = "Usage: uxsm aux <exec> ...\n\nInternal commands used by uxsm's systemd units.\n"
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, auxUsage)
		return errUsage
	}
	// `uxsm aux -h` pide la ayuda de aux; `uxsm aux exec -h`, la de exec.
	if wantsHelp(args[:1]) {
		fmt.Fprint(os.Stdout, auxUsage)
		return nil
	}

	switch args[0] {
	case "exec":
		return runAuxExec(args[1:])
	}
	return fmt.Errorf("unknown aux command %q", args[0])
}

// runAuxExec ejecuta el escritorio de una entrada: `uxsm aux exec bspwm.desktop`.
//
// Es el ExecStart= de uxsm-desktop@.service. Vuelve a leer la entrada en vez de
// recibir el comando desde start: así la unidad sólo necesita el ID (%i), y
// systemd muestra en su estado un nombre legible en lugar de un comando largo.
//
// Se sustituye por el programa con exec para que el proceso principal del
// servicio sea el propio escritorio: cuando el escritorio termina, termina el
// servicio.
func runAuxExec(args []string) error {
	fs := newFlagSet("aux exec", "<entry.desktop>",
		"Replace this process with the Exec= of a session entry.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}

	entry, err := desktopentry.Find("xsessions", fs.Arg(0))
	if err != nil {
		return err
	}
	argv, err := desktopentry.SplitExec(entry.Exec)
	if err != nil {
		return fmt.Errorf("%s: %w", entry.Path, err)
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("%s: %w", entry.Path, err)
	}

	return syscall.Exec(path, argv, os.Environ())
}
