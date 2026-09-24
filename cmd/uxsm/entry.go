package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/heizeisaburou/uxsm/internal/desktopentry"
	"github.com/heizeisaburou/uxsm/internal/dm"
	"github.com/heizeisaburou/uxsm/internal/sessionentry"
	"github.com/heizeisaburou/uxsm/internal/xdg"
)

// runEntry genera una entrada de sesión y la instala en dm.LocalXSessions:
//
//	uxsm entry [opciones] bspwm                  bspwm-uxsm.desktop, que apunta a bspwm.desktop
//	uxsm entry [opciones] --exec bspwm           bspwm-uxsm.desktop, con la orden de bspwm
//	uxsm entry [opciones] --exec -- mywm [args]  mywm-uxsm.desktop, con ese comando
//	uxsm entry [opciones] --plain bspwm          bspwm.desktop, de la tabla de escritorios
//	uxsm entry [opciones] --plain -- mywm [args] mywm.desktop, con ese comando
//
// Sólo escribe en dm.LocalXSessions: ni a la salida estándar ni a otro
// directorio, y menos en /usr/share/xsessions, que es de los paquetes. Sin -i
// sólo enseña qué fichero escribiría y con qué contenido.
func runEntry(args []string) error {
	fs := newFlagSet("entry", "[options] <name>\n"+
		"       uxsm entry [options] --exec <name> | --exec -- <command> [args...]\n"+
		"       uxsm entry [options] --plain <name> | --plain -- <command> [args...]",
		"Generate a session entry and install it in "+dm.LocalXSessions+".\n\n"+
			"  <name>          <name>-uxsm.desktop, which starts the session entry\n"+
			"                  <name>.desktop with `uxsm start <name>.desktop`\n"+
			"  --exec <name>   <name>-uxsm.desktop, which starts the command of\n"+
			"                  <name>.desktop, or the one in uxsm's table of known\n"+
			"                  desktops, with `uxsm start -D names -- command`\n"+
			"  --plain <name>  the plain <name>.desktop, without uxsm, from the table\n"+
			"  -- <command>    with --exec or --plain, an entry for that command,\n"+
			"                  named after its program\n\n"+
			"Without -i, it only shows the file it would write.")
	exec := fs.Bool("exec", false, "make a -uxsm entry that starts the command directly")
	plain := fs.Bool("plain", false, "make the plain entry, without uxsm")
	install := fs.Bool("i", false, "write the entry (needs root)")
	force := fs.Bool("f", false, "overwrite an entry with the same name in "+dm.LocalXSessions+",\nor hide one in another xsessions directory, such as a package's")
	forceNames := fs.Bool("force-names", false, "let -e drop the desktop names that are known for it")
	names := fs.String("D", "", "desktop `names` to add, separated by ':'")
	exclusive := fs.Bool("e", false, "use only the names given with -D, dropping the known ones")
	name := fs.String("N", "", "the `name` shown on the login screen")
	comment := fs.String("C", "", "the `comment` shown on the login screen")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	dashes := afterDashes(args, fs.NArg())
	if *exec && *plain || fs.NArg() == 0 || !dashes && fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}
	if dashes && !*exec && !*plain {
		return errors.New("an entry that points to another entry needs that entry's name; for a command, use --exec or --plain")
	}

	src, err := entrySource(fs.Args(), dashes, *exec, *plain)
	if err != nil {
		return err
	}
	opts := sessionentry.Options{Names: *names, Exclusive: *exclusive, ForceNames: *forceNames, Name: *name, Comment: *comment}
	var e *sessionentry.Entry
	switch {
	case *plain:
		e, err = src.Plain(opts)
	case *exec:
		e, err = src.UxsmExec(opts)
	default:
		e, err = src.Uxsm(opts)
	}
	if err != nil {
		return err
	}

	dest := filepath.Join(dm.LocalXSessions, e.ID)
	if err := checkDestination(dest, e.ID, *force); err != nil {
		return err
	}
	warnDisplayManager()

	content := e.Render()
	if !*install {
		// Sin dos puntos ni punto detrás de una ruta: así se copia de la
		// terminal con dos clics, sin arrastrar el signo.
		fmt.Printf("Would write this file, run it again with -i to write it (as root)\n  %s\n\n%s", dest, content)
		return nil
	}
	if err := os.MkdirAll(dm.LocalXSessions, 0o755); err != nil {
		return writeError(dest, err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		return writeError(dest, err)
	}
	fmt.Printf("Wrote %s\n", dest)
	return nil
}

// entrySource decide de dónde sale la entrada: un comando, una entrada que
// existe o la tabla de escritorios conocidos.
func entrySource(args []string, dashes, exec, plain bool) (*sessionentry.Source, error) {
	if dashes {
		return sessionentry.FromCommand(args)
	}
	id := strings.TrimSuffix(args[0], ".desktop") + ".desktop"
	name := strings.TrimSuffix(id, ".desktop")

	// La entrada normal sale siempre de la tabla: si ya hubiera una, no haría
	// falta generarla.
	if plain {
		return sessionentry.FromTable(name)
	}
	entry, err := desktopentry.Find(desktopentry.XSessions, id)
	if err == nil {
		return sessionentry.FromEntry(entry)
	}
	if exec {
		return sessionentry.FromTable(name)
	}
	// Una entrada de uxsm que apunta a otra necesita que la otra exista. No se
	// crea por su cuenta: se dice cómo seguir.
	if _, terr := sessionentry.FromTable(name); terr == nil {
		return nil, fmt.Errorf("there is no session entry %s to point to; create it first with `uxsm entry --plain %s`, or make one that starts the command directly with `uxsm entry --exec %s`", id, name, name)
	}
	return nil, fmt.Errorf("there is no session entry %s to point to, and %s is not in uxsm's table of known desktops; make one for its command with `uxsm entry --exec -- <command>`", id, name)
}

// checkDestination comprueba que escribir dest no pisa ni tapa otra entrada con
// el mismo nombre, salvo con -f: la de dest misma, o la de otro directorio de
// xsessions que ésta taparía, porque /usr/local/share va antes, como la de un
// paquete en /usr/share/xsessions.
func checkDestination(dest, id string, force bool) error {
	if force {
		return nil
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists; use -f to overwrite it", dest)
	}
	for _, d := range xdg.DataDirs() {
		p := filepath.Join(d, desktopentry.XSessions, id)
		if filepath.Dir(p) == dm.LocalXSessions {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("there is already a session entry %s in %s, and %s would hide it: the display manager and uxsm start would use the new one; use -f to do it anyway", id, filepath.Dir(p), dest)
		}
	}
	return nil
}

// warnDisplayManager avisa, por la salida de error, si el display manager en
// uso no lee dm.LocalXSessions: la entrada no saldría en la pantalla de inicio.
func warnDisplayManager() {
	r, err := dm.Active()
	if err != nil || r.Reads(dm.LocalXSessions) {
		return
	}
	fmt.Fprintf(os.Stderr, "uxsm: warning: %s, the display manager in use, does not read %s,\n"+
		"so this entry will not show on the login screen; to fix it: uxsm setup sessions-dir\n",
		r.Name, dm.LocalXSessions)
}

// writeError explica un error al escribir la entrada.
func writeError(dest string, err error) error {
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("writing %s: %w (run it as root)", dest, err)
	}
	return fmt.Errorf("writing %s: %w", dest, err)
}
