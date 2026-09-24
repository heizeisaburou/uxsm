package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/heizeisaburou/uxsm/internal/desktopentry"
	"github.com/heizeisaburou/uxsm/internal/session"
	"github.com/heizeisaburou/uxsm/internal/systemd"
)

// runStart arranca una sesión, de una entrada o de un comando, como uwsm:
//
//	uxsm start [-e] [-D nombres] bspwm.desktop
//	uxsm start [-e] [-D nombres] [--] bspwm [argumentos…]
//
// Es lo que ejecuta el display manager, así que el proceso que vigila es éste:
//  1. Decide qué arranca (resolveTarget): la entrada, buscada en los directorios
//     xsessions, o el comando. Comprueba también que hay bus de sesión de
//     D-Bus. Así falla ya si algo no existe o está mal, antes de tocar systemd.
//  2. Calcula la identidad de la sesión ―los nombres del escritorio y las
//     variables XDG_* que los dicen― con -D y -e.
//  3. Espera a que no quede nada de una sesión anterior que todavía se esté
//     apagando: su limpieza borraría los ficheros de ésta.
//  4. Guarda en $XDG_RUNTIME_DIR/uxsm el entorno que le ha dado el display
//     manager y esa identidad, y con un comando, también el comando. Con eso,
//     uxsm-env@.service monta el entorno de la sesión en el gestor antes de que
//     arranque el escritorio, y lo limpia al cerrar. Deja ahí también la marca
//     que dice si esta sesión lanza el autostart XDG (markAutostart).
//  5. Arranca uxsm-bindpid@<pid>.service con su propio PID, para que la sesión se
//     apague si el display manager mata este proceso.
//  6. Se sustituye por `systemctl --user start --wait uxsm-desktop@<id>.service`,
//     que no vuelve hasta que el escritorio termina. Con exec el PID no cambia,
//     así que el PID que vigila bindpid sigue siendo el de la sesión.
func runStart(args []string) error {
	fs := newFlagSet("start", "[-e] [-D names] [--no-autostart] <entry.desktop>\n"+
		"       uxsm start [-e] [-D names] [--no-autostart] [--] <command> [args...]",
		"Start an X11 session, running its desktop as a systemd user service: the\n"+
			"Exec= of a session entry from the xsessions directories, or a command.\n"+
			"An argument ending in .desktop is an entry; anything else, or anything\n"+
			"after --, is a command.")
	namesFlag := fs.String("D", "", "desktop `names` for XDG_CURRENT_DESKTOP, separated by ':'")
	exclusive := fs.Bool("e", false, "use only the names given with -D, discarding the existing\n"+
		"XDG_CURRENT_DESKTOP and the entry's DesktopNames=")
	noAutostart := fs.Bool("no-autostart", false, "do not start the XDG autostart entries of the session;\n"+
		"uxsm starts them by default, as uwsm does")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return errUsage
	}
	target, err := resolveTarget(fs.Args(), afterDashes(args, fs.NArg()))
	if err != nil {
		return err
	}
	if err := systemd.CheckInstance(target.id); err != nil {
		return err
	}
	if err := systemd.CheckUserBus(); err != nil {
		return err
	}

	names, err := session.DesktopNames(session.NamesOptions{
		Current:    os.Getenv("XDG_CURRENT_DESKTOP"),
		Entry:      target.names,
		Flag:       *namesFlag,
		Exclusive:  *exclusive,
		Executable: filepath.Base(target.argv[0]),
	})
	if errors.Is(err, session.ErrBadNames) {
		fmt.Fprintf(os.Stderr, "uxsm start: %v\n", err)
		return errUsage
	}
	if err != nil {
		return err
	}
	identity := session.IdentityVars(names)

	if err := waitForPreviousSession(previousSessionTimeout); err != nil {
		return err
	}

	dir, err := session.RuntimeDir()
	if err != nil {
		return err
	}
	if err := session.WriteEnvFile(filepath.Join(dir, session.LoginFile), session.FilterEnv(os.Environ())); err != nil {
		return fmt.Errorf("saving the login environment: %w", err)
	}
	if err := session.WriteEnvFile(filepath.Join(dir, session.IdentityFile), identity); err != nil {
		return fmt.Errorf("saving the session identity: %w", err)
	}
	if target.command {
		if err := session.WriteEnvFile(filepath.Join(dir, session.CommandFile), target.argv); err != nil {
			return fmt.Errorf("saving the command: %w", err)
		}
	}
	// Por si quedara encendida de una sesión anterior que no llegó a limpiar:
	// con ella encendida, ésta se daría por lista sin escritorio en pantalla.
	if err := session.ClearReady(); err != nil {
		return fmt.Errorf("clearing the ready signal of a previous session: %w", err)
	}
	if err := markAutostart(dir, *noAutostart); err != nil {
		return fmt.Errorf("deciding on the XDG autostart: %w", err)
	}

	if err := systemd.Start(systemd.BindPIDUnit(os.Getpid())); err != nil {
		return fmt.Errorf("binding the session to its process: %w", err)
	}
	return systemd.ExecStartWait(systemd.DesktopUnit(target.id))
}

// markAutostart deja escrito en el directorio de runtime si esta sesión lanza
// el autostart XDG. Es lo que mira `uxsm aux autostart`, el ExecStartPost= del
// escritorio.
//
// Se lanza siempre salvo que se diga que no, como en uwsm: de una sesión con
// systemd se espera que las entradas de autostart arranquen solas. uxsm start
// no mira aquí qué escritorio es; de eso sabe uxsm entry, que pone
// --no-autostart en las entradas de los escritorios que lanzan el suyo.
func markAutostart(dir string, disabled bool) error {
	path := filepath.Join(dir, session.AutostartFile)
	if disabled {
		fmt.Println("Not starting the XDG autostart entries of the session: --no-autostart.")
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	return os.WriteFile(path, []byte("uxsm start, without --no-autostart\n"), 0o600)
}

// startTarget es lo que arranca uxsm start.
type startTarget struct {
	// id es la instancia de las unidades de la sesión: el ID de la entrada,
	// "bspwm.desktop", o el nombre del programa del comando, "bspwm".
	id string
	// argv es lo que ejecutará el escritorio.
	argv []string
	// names son los DesktopNames= de la entrada; con un comando, ninguno.
	names []string
	// command dice si es un comando: entonces uxsm start lo guarda para que lo
	// lea uxsm aux exec, que con una entrada vuelve a leer la entrada.
	command bool
}

// resolveTarget decide qué arranca uxsm start a partir de los argumentos que
// quedan detrás de las opciones. dashes dice si venían detrás de "--".
//
// Un único argumento acabado en .desktop, sin "--", es una entrada; todo lo
// demás es un comando, con sus argumentos. Es la misma regla de uwsm, y "--"
// sirve para lo mismo que allí: pasarle al programa argumentos que empiezan
// por "-", que si no se tomarían por opciones de uxsm.
func resolveTarget(args []string, dashes bool) (*startTarget, error) {
	if !dashes && len(args) == 1 && strings.HasSuffix(args[0], ".desktop") {
		entry, err := desktopentry.Find(desktopentry.XSessions, args[0])
		if err != nil {
			return nil, err
		}
		argv, err := desktopentry.SplitExec(entry.Exec)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Path, err)
		}
		return &startTarget{id: entry.ID, argv: argv, names: entry.DesktopNames}, nil
	}

	// Como con una entrada que no existe: mejor fallar ahora que cuando el
	// servicio del escritorio intente ejecutarlo.
	if _, err := exec.LookPath(args[0]); err != nil {
		return nil, err
	}
	return &startTarget{id: filepath.Base(args[0]), argv: args, command: true}, nil
}

// afterDashes dice si los n últimos argumentos de args venían detrás de "--".
// Hace falta porque el paquete flag se come el "--" sin avisar.
func afterDashes(args []string, n int) bool {
	i := len(args) - n
	return i > 0 && args[i-1] == "--"
}

// previousSessionTimeout es cuánto espera uxsm start a que termine de apagarse
// una sesión anterior. Una sesión que se apaga tarda poco más de lo que tarda su
// limpieza; si pasado este tiempo sigue ahí, es que hay otra sesión en marcha.
const previousSessionTimeout = 10 * time.Second

// sessionUnits son las unidades de una sesión gráfica de este usuario: las de
// uxsm y las de uwsm, que es el otro que hace esto. Si alguna sigue viva, hay
// una sesión arrancada o todavía apagándose.
//
// Dos sesiones gráficas de un mismo usuario no encajan, las gestione quien las
// gestione: el gestor de systemd es uno por usuario, así que comparten
// graphical-session.target y el entorno. Cerrar una apagaría el target de la
// otra, y la limpieza del entorno de una borraría lo que la otra acaba de
// poner.
//
// Aquí van nombres concretos y no graphical-session.target, que es de systemd y
// lo enciende cualquiera: el envoltorio de sesión de NixOS lo activa antes de
// ejecutar el Exec= de la entrada, así que mirarlo hacía que uxsm se tomara a
// sí mismo por una sesión anterior y se negara a arrancar.
var sessionUnits = append(uxsmUnits,
	// Las de uwsm, que gestiona así las sesiones de Wayland.
	"wayland-session-shutdown.target", "wayland-wm@*.service", "wayland-wm-env@*.service",
	"wayland-session@*.target", "wayland-session-pre@*.target", "wayland-session-bindpid@*.service",
)

// uxsmUnits son las de una sesión de uxsm, las que dice `uxsm check is-active`.
var uxsmUnits = []string{
	"uxsm-shutdown.target",
	"uxsm-desktop@*.service", "uxsm-env@*.service", "uxsm-session@*.target", "uxsm-bindpid@*.service",
}

// waitForPreviousSession espera hasta timeout a que no quede viva ninguna unidad
// de otra sesión gráfica.
//
// Hace falta porque el cierre de una sesión no es instantáneo: su servicio de
// entorno limpia el gestor y borra los ficheros de $XDG_RUNTIME_DIR/uxsm mientras
// se para. Si una sesión nueva empezara en ese momento ―un autologin rápido―, esa
// limpieza le borraría los ficheros recién escritos.
func waitForPreviousSession(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		live, err := systemd.LiveUnits(sessionUnits...)
		if err != nil {
			return err
		}
		if len(live) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("another graphical session is still running: %s", strings.Join(live, ", "))
		}
		time.Sleep(100 * time.Millisecond)
	}
}
