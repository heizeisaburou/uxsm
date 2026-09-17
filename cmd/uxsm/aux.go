package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/heizeisaburou/uxsm/internal/desktopentry"
	"github.com/heizeisaburou/uxsm/internal/pidwait"
)

// auxCommands son las subórdenes internas, las que llaman las unidades de
// systemd de uxsm desde sus Exec*=. Salen en `uxsm aux -h`, no en la ayuda
// general.
var auxCommands = group{
	name:        "uxsm aux",
	description: "Internal commands used by uxsm's systemd units.",
	commands: []command{
		{"exec", "replace this process with the Exec= of a session entry", runAuxExec, false},
		{"waitpid", "wait until a process exits", runAuxWaitPID, false},
	},
}

// runAux reparte `uxsm aux <suborden>` igual que se reparte `uxsm <suborden>`.
func runAux(args []string) error {
	return auxCommands.dispatch(args)
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

// runAuxWaitPID espera a que termine un proceso: `uxsm aux waitpid 1234`.
//
// Es el ExecStart= de uxsm-bindpid@.service, con el PID del proceso de la sesión
// como instancia. Cuando vuelve, el servicio termina y su OnSuccess= arranca
// uxsm-shutdown.target, que apaga la sesión.
func runAuxWaitPID(args []string) error {
	fs := newFlagSet("aux waitpid", "<pid>", "Wait until a process exits; any process, not only a child.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}
	pid, err := strconv.Atoi(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("invalid PID %q", fs.Arg(0))
	}
	return pidwait.Wait(pid)
}
