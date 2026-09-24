package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/heizeisaburou/uxsm/internal/appunit"
	"github.com/heizeisaburou/uxsm/internal/desktopentry"
)

// runApp lanza una aplicación en su propia unidad de systemd:
//
//	uxsm app -- kitty
//	uxsm app firefox.desktop
//	uxsm app firefox.desktop:new-private-window
//	uxsm app -s b -- fcitx5
//
// Es lo mismo que hace `uwsm app` en Wayland. Sin esto, lo que arranca un
// escritorio cuelga del escritorio y se ve todo junto; con esto, cada aplicación
// tiene su unidad dentro de uno de los slices de la sesión: se ve por separado
// en systemctl, se le pueden poner límites, su registro va al diario con su
// nombre y se para con la sesión.
func runApp(args []string) error {
	fs := newFlagSet("app", "[-s slice] [-t scope|service] [-p Key=Value] [options] [--] <command> [args...]\n"+
		"       uxsm app [options] <entry.desktop>[:action] [files or URLs...]",
		"Run an application in its own systemd unit, inside the graphical slices of\n"+
			"the session: it shows up on its own and stops with the session.\n"+
			"An argument ending in .desktop is a desktop entry, optionally with an\n"+
			"action after ':'; anything else, or anything after --, is a command.")
	slice := fs.String("s", "", "`slice` of the session: a (applications, the default),\n"+
		"b (background), s (session), or a name ending in .slice")
	unitType := fs.String("t", "scope", "unit `type`: scope, the default, or service")
	appName := fs.String("a", "", "`name` of the application within the unit name")
	unitName := fs.String("u", "", "the whole `unit` name, instead of the generated one")
	description := fs.String("d", "", "unit `description`")
	silent := fs.String("S", "", "throw away the application's `output`: out, err or both;\n"+
		"only for a service, a scope inherits the output of its caller")
	var properties stringList
	fs.Var(&properties, "p", "systemd `property` of the unit, as Key=Value, like systemd-run\n"+
		"takes them: TimeoutStopSec=5, MemoryMax=2G… Can be repeated")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return errUsage
	}

	o := appunit.Options{
		Properties:  properties,
		AppName:     *appName,
		UnitName:    *unitName,
		Description: *description,
		Silent:      *silent,
	}
	switch *unitType {
	case "scope":
	case "service":
		o.Service = true
	default:
		fmt.Fprintf(os.Stderr, "uxsm app: -t is %q, but a unit is either a scope or a service\n", *unitType)
		return errUsage
	}
	if o.Silent != "" && !o.Service {
		fmt.Fprintln(os.Stderr, "uxsm app: -S only works with -t service: a scope inherits the output of its caller")
		return errUsage
	}

	var err error
	if o.Slice, err = appunit.Slice(*slice); err != nil {
		return err
	}
	if err := resolveApp(&o, fs.Args(), afterDashes(args, fs.NArg())); err != nil {
		return err
	}

	runArgs, err := o.RunArgs()
	if err != nil {
		return err
	}
	path, err := exec.LookPath(runArgs[0])
	if err != nil {
		return err
	}
	// Con exec, el scope se queda con este proceso: la aplicación es hija de
	// quien pidió lanzarla, que es lo que un scope significa.
	return syscall.Exec(path, runArgs, os.Environ())
}

// resolveApp decide qué se lanza: una entrada de aplicación, con su acción si se
// pide, o un comando tal cual. dashes dice si los argumentos venían detrás de
// "--", que entonces son siempre un comando.
func resolveApp(o *appunit.Options, args []string, dashes bool) error {
	id, action, _ := strings.Cut(args[0], ":")
	if dashes || !strings.HasSuffix(id, ".desktop") {
		if _, err := exec.LookPath(args[0]); err != nil {
			return err
		}
		o.Argv = args
		return nil
	}

	entry, err := desktopentry.Find(desktopentry.Applications, id)
	if err != nil {
		return err
	}
	if entry.Terminal {
		return fmt.Errorf("%s asks to run inside a terminal, which uxsm app does not do yet: "+
			"run it with your terminal, as `uxsm app -- <terminal> -e %s`", entry.ID, entry.Exec)
	}

	command := entry.Exec
	if action != "" {
		a, ok := entry.Actions[action]
		if !ok {
			return fmt.Errorf("%s has no action %q", entry.ID, action)
		}
		if a.Exec == "" {
			return fmt.Errorf("the action %q of %s has no Exec=", action, entry.ID)
		}
		command = a.Exec
	}

	argv, err := desktopentry.SplitExecFields(command, desktopentry.Fields{
		Args: args[1:],
		Icon: entry.Icon,
		Name: entry.Name,
		Path: entry.Path,
	})
	if err != nil {
		return fmt.Errorf("%s: %w", entry.Path, err)
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return err
	}

	o.Argv = argv
	o.WorkingDir = entry.WorkingDir
	if o.Entry == "" {
		o.Entry = strings.TrimSuffix(entry.ID, ".desktop")
	}
	if o.Description == "" {
		o.Description = first(entry.Name, entry.Comment)
	}
	return nil
}

// stringList es una opción que se puede repetir, como el -p de systemd-run: cada
// vez que aparece, añade su valor a la lista.
type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, " ") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

// first devuelve el primer valor que no está vacío.
func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
