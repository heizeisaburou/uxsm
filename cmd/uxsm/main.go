// Command uxsm arranca y gestiona sesiones gráficas X11 con systemd --user,
// como uwsm lo hace en Wayland.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// version la pone el Makefile al compilar: go build -ldflags "-X main.version=…".
var version = "dev"

// command es una suborden: su nombre, una línea de ayuda, la función que la
// ejecuta con los argumentos que vienen detrás del nombre y si se oculta en la
// ayuda general.
type command struct {
	name    string
	summary string
	run     func(args []string) error
	hidden  bool
}

// group es un conjunto de subórdenes con su propia ayuda. uxsm tiene dos: el
// de primer nivel (start, stop…) y el de aux (exec, waitpid). Los dos reparten
// con el mismo código, así que se comportan igual.
type group struct {
	// name es cómo se escribe el grupo en la línea de órdenes: "uxsm" o "uxsm aux".
	name        string
	description string
	commands    []command
}

// rootCommands son las subórdenes de uxsm, en el orden en que salen en la
// ayuda. aux está oculta: la llaman las unidades de systemd, no las personas,
// pero se ejecuta igual que las demás.
var rootCommands = group{
	name:        "uxsm",
	description: "Start and manage X11 sessions under systemd --user.",
	commands: []command{
		{"start", "start an X11 session from a session entry or a command", runStart, false},
		{"stop", "stop the running session", runStop, false},
		{"entry", "generate a session entry and install it", runEntry, false},
		{"check", "check that the system is ready for uxsm", runCheck, false},
		{"setup", "change the system so that uxsm works fully", runSetup, false},
		{"version", "print the version", runVersion, false},
		{"aux", "internal commands used by uxsm's systemd units", runAux, true},
	},
}

func main() {
	os.Exit(exitCode(rootCommands.dispatch(os.Args[1:])))
}

// dispatch selecciona la suborden a partir del primer argumento y le pasa
// el resto tal cual.
//
// `help`, `-h` y `--help` sólo se interpretan aquí si aparecen en primera
// posición; cualquier ayuda posterior corresponde a la suborden.
func (g group) dispatch(args []string) error {
	if len(args) == 0 {
		g.usage(os.Stderr)
		return errUsage
	}

	name, rest := args[0], args[1:]
	if name == "help" || isHelpFlag(name) {
		g.usage(os.Stdout)
		return nil
	}
	for _, c := range g.commands {
		if c.name == name {
			return c.run(rest)
		}
	}

	fmt.Fprintf(os.Stderr, "%s: unknown command %q\n\n", g.name, name)
	g.usage(os.Stderr)
	return errUsage
}

// usage escribe la ayuda del grupo, sin las subórdenes ocultas.
func (g group) usage(w io.Writer) {
	fmt.Fprintf(w, "Usage: %s <command> [options]\n\n%s\n\nCommands:\n", g.name, g.description)
	for _, c := range g.commands {
		if !c.hidden {
			fmt.Fprintf(w, "  %-12s %s\n", c.name, c.summary)
		}
	}
	fmt.Fprintf(w, "\nRun \"%s <command> -h\" for help on a command.\n", g.name)
}

// errUsage marca los errores de argumentos, que ya han enseñado su ayuda.
var errUsage = errors.New("usage")

// exitCode traduce el resultado de una suborden a código de salida: 0 si va
// bien o se ha pedido ayuda, 2 si los argumentos están mal y 1 con cualquier
// otro error, que además se escribe.
func exitCode(err error) int {
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errUsage):
		return 2
	default:
		fmt.Fprintf(os.Stderr, "uxsm: %v\n", err)
		return 1
	}
}

// wantsHelp dice si args pide ayuda en cualquier posición antes de "--".
//
// flag deja de leer opciones en el primer argumento que no lo es, así que por
// sí solo no vería el -h de `uxsm start bspwm.desktop -h`. Mirar toda la línea
// permite añadir -h al final de una orden a medio escribir y ver su ayuda. Lo
// que va detrás de "--" es del programa que se lanza, no de uxsm.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if isHelpFlag(a) {
			return true
		}
	}
	return false
}

// isHelpFlag dice si a es una forma de pedir ayuda. Son las cuatro que reconoce
// el paquete flag, para que valgan las mismas en cualquier sitio: `uxsm -help`
// y `uxsm start bspwm.desktop -help`.
func isHelpFlag(a string) bool {
	switch a {
	case "-h", "-help", "--h", "--help":
		return true
	}
	return false
}

// parseFlags lee las opciones de una suborden. Si en cualquier parte se pide
// ayuda, la enseña por la salida estándar y devuelve flag.ErrHelp, que acaba
// con código 0. Si una opción está mal, flag ya ha escrito el error y la ayuda
// en la salida de error, y devuelve errUsage, que acaba con código 2.
func parseFlags(fs *flag.FlagSet, args []string) error {
	if wantsHelp(args) {
		fs.SetOutput(os.Stdout)
		fs.Usage()
		return flag.ErrHelp
	}
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	return nil
}

// newFlagSet crea el conjunto de opciones de una suborden con su propia ayuda:
// la línea de uso, una descripción y las opciones que tenga.
func newFlagSet(name, usageLine, description string) *flag.FlagSet {
	fs := flag.NewFlagSet("uxsm "+name, flag.ContinueOnError)
	fs.Usage = func() {
		w := fs.Output()
		fmt.Fprintf(w, "Usage: uxsm %s %s\n\n%s\n", name, usageLine, description)
		hasFlags := false
		fs.VisitAll(func(*flag.Flag) { hasFlags = true })
		if hasFlags {
			fmt.Fprint(w, "\nOptions:\n")
			fs.PrintDefaults()
		}
	}
	return fs
}

func runVersion(args []string) error {
	fs := newFlagSet("version", "", "Print the uxsm version.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	fmt.Println(version)
	return nil
}
