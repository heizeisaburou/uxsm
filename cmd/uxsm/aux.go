package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/heizeisaburou/uxsm/internal/desktopentry"
	"github.com/heizeisaburou/uxsm/internal/pidwait"
	"github.com/heizeisaburou/uxsm/internal/session"
	"github.com/heizeisaburou/uxsm/internal/sessionenv"
)

// auxCommands son las subórdenes internas, las que llaman las unidades de
// systemd de uxsm desde sus Exec*=. Salen en `uxsm aux -h`, no en la ayuda
// general.
var auxCommands = group{
	name:        "uxsm aux",
	description: "Internal commands used by uxsm's systemd units.",
	commands: []command{
		{"exec", "replace this process with the desktop of the session", runAuxExec, false},
		{"waitpid", "wait until a process exits", runAuxWaitPID, false},
		{"prepare-env", "set up the session environment in the systemd user manager", runAuxPrepareEnv, false},
		{"cleanup-env", "restore the systemd user manager environment from before the session", runAuxCleanupEnv, false},
	},
}

// runAux reparte `uxsm aux <suborden>` igual que se reparte `uxsm <suborden>`.
func runAux(args []string) error {
	return auxCommands.dispatch(args)
}

// runAuxExec ejecuta el escritorio de la sesión: `uxsm aux exec bspwm.desktop`
// o, si la sesión se arrancó con un comando, `uxsm aux exec bspwm`.
//
// Es el ExecStart= de uxsm-desktop@.service, con la instancia como argumento.
// No recibe el comando por la línea de órdenes: con una entrada, la vuelve a
// leer; con un comando, lee el que guardó uxsm start. Así la unidad sólo
// necesita la instancia (%i), y systemd muestra en su estado un nombre legible
// en lugar de un comando largo.
//
// Se sustituye por el programa con exec para que el proceso principal del
// servicio sea el propio escritorio: cuando el escritorio termina, termina el
// servicio.
func runAuxExec(args []string) error {
	fs := newFlagSet("aux exec", "<entry.desktop | command-name>",
		"Replace this process with the desktop of the session: the Exec= of a\n"+
			"session entry, or the command saved by uxsm start.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}

	argv, err := desktopCommand(fs.Arg(0))
	if err != nil {
		return err
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	return syscall.Exec(path, argv, os.Environ())
}

// desktopCommand devuelve la orden del escritorio de la instancia id: el Exec=
// de la entrada si id acaba en .desktop, o el comando que guardó uxsm start.
func desktopCommand(id string) ([]string, error) {
	if strings.HasSuffix(id, ".desktop") {
		entry, err := desktopentry.Find(desktopentry.XSessions, id)
		if err != nil {
			return nil, err
		}
		argv, err := desktopentry.SplitExec(entry.Exec)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Path, err)
		}
		return argv, nil
	}

	dir, err := session.RuntimeDir()
	if err != nil {
		return nil, err
	}
	argv, err := session.ReadEnvFile(filepath.Join(dir, session.CommandFile))
	if err != nil {
		return nil, fmt.Errorf("reading the command saved by uxsm start: %w", err)
	}
	// Sólo hay una sesión cada vez, pero un fichero que no sea de esta
	// instancia es un resto de otra: mejor no ejecutarlo.
	if len(argv) == 0 || filepath.Base(argv[0]) != id {
		return nil, fmt.Errorf("the command saved by uxsm start, %q, is not the one of %s", argv, id)
	}
	return argv, nil
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

// runAuxPrepareEnv monta el entorno de la sesión en el gestor: `uxsm aux prepare-env`.
//
// Es el ExecStart= de uxsm-env@.service, que corre antes que el escritorio. Lee lo
// que dejó uxsm start en $XDG_RUNTIME_DIR/uxsm; el trabajo está en
// sessionenv.Prepare.
func runAuxPrepareEnv(args []string) error {
	fs := newFlagSet("aux prepare-env", "", "Set up the session environment in the systemd user manager.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return errUsage
	}
	return sessionenv.Prepare()
}

// runAuxCleanupEnv deja el entorno del gestor como estaba: `uxsm aux cleanup-env`.
//
// Es el ExecStopPost= de uxsm-env@.service, así que corre siempre que se para el
// servicio, venga de donde venga el cierre. El trabajo está en sessionenv.Cleanup.
func runAuxCleanupEnv(args []string) error {
	fs := newFlagSet("aux cleanup-env", "", "Restore the systemd user manager environment from before the session.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return errUsage
	}
	return sessionenv.Cleanup()
}
