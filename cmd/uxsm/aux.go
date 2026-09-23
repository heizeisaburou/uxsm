package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/heizeisaburou/uxsm/internal/desktopentry"
	"github.com/heizeisaburou/uxsm/internal/pidwait"
	"github.com/heizeisaburou/uxsm/internal/session"
	"github.com/heizeisaburou/uxsm/internal/sessionenv"
	"github.com/heizeisaburou/uxsm/internal/systemd"
	"github.com/heizeisaburou/uxsm/internal/x11"
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
		{"wait-ready", "wait until the session is ready", runAuxWaitReady, false},
		{"autostart", "start the XDG autostart entries of the session", runAuxAutostart, false},
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

// readyInterval es cada cuánto se comprueba si la sesión ya está lista. Es una
// comprobación barata ―dos propiedades por un socket de unix y un fichero― y
// medio segundo de escritorio parado se nota, así que se mira a menudo.
const readyInterval = 100 * time.Millisecond

// runAuxWaitReady espera a que la sesión esté lista: `uxsm aux wait-ready`.
//
// Es el ExecStartPost= de uxsm-desktop@.service. systemd no da por arrancado el
// servicio hasta que termina su ExecStartPost=, y detrás del servicio van
// uxsm-session@.target y graphical-session.target: así lo que arranque con la
// sesión encuentra un escritorio ya en pantalla, y no un sitio donde todavía no
// se puede colocar nada.
//
// La espera no tiene límite propio: lo pone TimeoutStartSec= en la unidad. Si
// se acaba, systemd mata esta espera, el servicio falla y su OnFailure= apaga
// la sesión, que es lo que devuelve el control al display manager.
func runAuxWaitReady(args []string) error {
	fs := newFlagSet("aux wait-ready", "",
		"Wait until the session is ready: either an EWMH window manager takes\n"+
			"over the X display, or the desktop runs uxsm finalize.")
	timeout := fs.Duration("timeout", 0, "give up after this `duration`; zero waits with no limit of its own")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return errUsage
	}

	reason, err := waitReady(*timeout)
	if err != nil {
		return err
	}
	fmt.Println(reason)
	return nil
}

// waitReady espera a que se encienda la señal de que la sesión está lista y
// devuelve por qué se encendió.
//
// Son dos caminos a la vez, y vale el primero que llegue: el gestor de ventanas
// EWMH, que uxsm ve mirando el servidor X, y `uxsm finalize`, que ejecuta el
// escritorio. Encenderla es una sola operación del sistema (session.SignalReady),
// así que si los dos llegan a la vez sólo cuenta uno.
func waitReady(timeout time.Duration) (string, error) {
	// El escritorio puede haber llamado a uxsm finalize antes incluso de que
	// esta espera empiece: entonces no hay nada que esperar ni a qué conectarse.
	if on, reason, err := session.Ready(); err != nil || on {
		return reason, err
	}

	c, err := x11.Dial("")
	if err != nil {
		return "", err
	}
	defer c.Close()

	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		wm, err := c.Manager()
		if err != nil {
			return "", err
		}
		if wm != nil {
			if _, err := session.SignalReady("window manager ready: " + wm.String()); err != nil {
				return "", err
			}
			// Si uxsm finalize se adelantó por muy poco, la razón que vale es
			// la suya, que es la que quedó escrita.
			_, reason, err := session.Ready()
			return reason, err
		}
		if on, reason, err := session.Ready(); err != nil || on {
			return reason, err
		}
		if !deadline.IsZero() && !time.Now().Add(readyInterval).Before(deadline) {
			return "", fmt.Errorf("the session was not ready after %s: no EWMH window manager took over "+
				"the X display, and the desktop did not run uxsm finalize", timeout)
		}
		time.Sleep(readyInterval)
	}
}

// runAuxAutostart arranca el autostart XDG de la sesión, si le toca a uxsm:
// `uxsm aux autostart bspwm.desktop`.
//
// Es el segundo ExecStartPost= de uxsm-desktop@.service, detrás de la espera al
// gestor de ventanas, así que las aplicaciones de autostart arrancan con el
// escritorio ya en pantalla.
//
// Sólo arranca el target si uxsm start dejó la marca que dice que el autostart
// le toca a uxsm; si no está, no hace nada, que es lo que hay que hacer en un
// escritorio que lanza el suyo. La decisión no puede ir en un Condition= de una
// unidad: las dependencias de una unidad se resuelven al montar el trabajo,
// antes de comprobar las condiciones, así que el autostart arrancaría igual
// aunque la unidad se saltara.
//
// Y tiene que arrancarlo una unidad nuestra, no `systemctl start
// xdg-desktop-autostart.target`: el target estándar lleva RefuseManualStart=,
// así que sólo se puede arrancar como dependencia de otra unidad.
func runAuxAutostart(args []string) error {
	fs := newFlagSet("aux autostart", "<entry.desktop | command-name>",
		"Start the XDG autostart entries of the session, if uxsm start decided\n"+
			"that starting them is up to uxsm.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}

	dir, err := session.RuntimeDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, session.AutostartFile)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	fmt.Println("Starting the XDG autostart entries of the session.")
	return systemd.StartNoBlock(systemd.AutostartTarget(fs.Arg(0)))
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
