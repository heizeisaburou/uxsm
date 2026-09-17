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

// command es una suborden: su nombre, una línea de ayuda y la función que la
// ejecuta con los argumentos que vienen detrás del nombre.
type command struct {
	name    string
	summary string
	run     func(args []string) error
}

// commands son las subórdenes visibles, en el orden en que salen en la ayuda.
// aux no está: la llaman las unidades de systemd, no las personas.
var commands = []command{
	{"start", "start an X11 session from a session entry", runStart},
	{"version", "print the version", runVersion},
}

func main() {
	os.Exit(run(os.Args[1:]))
}

// run elige la suborden y traduce su resultado a código de salida: 0 si va
// bien, 2 si los argumentos están mal y 1 con cualquier otro error.
func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}

	name, rest := args[0], args[1:]
	switch name {
	case "-h", "--help", "help":
		usage(os.Stdout)
		return 0
	case "aux":
		return exitCode(runAux(rest))
	}
	for _, c := range commands {
		if c.name == name {
			return exitCode(c.run(rest))
		}
	}

	fmt.Fprintf(os.Stderr, "uxsm: unknown command %q\n\n", name)
	usage(os.Stderr)
	return 2
}

// errUsage marca los errores de argumentos, que ya han enseñado su ayuda.
var errUsage = errors.New("usage")

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

func usage(w io.Writer) {
	fmt.Fprint(w, "Usage: uxsm <command> [options]\n\nCommands:\n")
	for _, c := range commands {
		fmt.Fprintf(w, "  %-9s %s\n", c.name, c.summary)
	}
	fmt.Fprint(w, "\nRun \"uxsm <command> -h\" for help on a command.\n")
}

// wantsHelp dice si args pide ayuda en cualquier posición antes de "--".
//
// flag deja de leer opciones en el primer argumento que no lo es, así que por
// sí solo no vería el -h de `uxsm start bspwm.desktop -h`. Mirar toda la línea
// permite añadir -h al final de una orden a medio escribir y ver su ayuda. Lo
// que va detrás de "--" es del programa que se lanza, no de uxsm.
func wantsHelp(args []string) bool {
	for _, a := range args {
		switch a {
		case "--":
			return false
		case "-h", "-help", "--h", "--help":
			return true
		}
	}
	return false
}

// parseFlags lee las opciones de una suborden. Si en cualquier parte se pide
// ayuda, la enseña por la salida estándar y devuelve flag.ErrHelp, que acaba
// con código 0. Si no, los errores de opciones y su ayuda van a la salida de
// error.
func parseFlags(fs *flag.FlagSet, args []string) error {
	if wantsHelp(args) {
		fs.SetOutput(os.Stdout)
		fs.Usage()
		return flag.ErrHelp
	}
	fs.SetOutput(os.Stderr)
	return fs.Parse(args)
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
