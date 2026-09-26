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
//	uxsm entry [opciones] --plain --from-table bspwm  bspwm.desktop, de la tabla
//	uxsm entry [opciones] --plain -- mywm [args] mywm.desktop, con ese comando
//
// La fuente se dice siempre: una entrada instalada, la tabla de escritorios
// conocidos con --from-table, o un comando detrás de --. Además, la tabla
// completa lo que la fuente no diga, y con --no-table no lo completa.
//
// Sólo escribe en dm.LocalXSessions: ni a la salida estándar ni a otro
// directorio, y menos en /usr/share/xsessions, que es de los paquetes. Sin -i
// sólo enseña qué fichero escribiría y con qué contenido.
func runEntry(args []string) error {
	fs := newFlagSet("entry", "[options] <name>\n"+
		"       uxsm entry [options] --exec <name> | --exec -- <command> [args...]\n"+
		"       uxsm entry [options] --plain --from-table <name> | --plain -- <command> [args...]",
		"Generate a session entry and install it in "+dm.LocalXSessions+".\n\n"+
			"  <name>          <name>-uxsm.desktop, which starts the session entry\n"+
			"                  <name>.desktop with `uxsm start <name>.desktop`\n"+
			"  --exec <name>   <name>-uxsm.desktop, which starts the command of\n"+
			"                  <name>.desktop with `uxsm start -D names -- command`\n"+
			"  --plain --from-table <name>\n"+
			"                  the plain <name>.desktop, without uxsm, from uxsm's\n"+
			"                  table of known desktops\n"+
			"  -- <command>    with --exec or --plain, an entry for that command,\n"+
			"                  named after its program\n\n"+
			"The source is always said: an installed entry, uxsm's table of known\n"+
			"desktops with --from-table, or a command after --. The table also fills\n"+
			"in what the source does not say ―desktop names, name, comment―, and\n"+
			"--no-table leaves it out of that too.\n\n"+
			"Without -i, it only shows the file it would write.")
	exec := fs.Bool("exec", false, "make a -uxsm entry that starts the command directly")
	plain := fs.Bool("plain", false, "make the plain entry, without uxsm")
	install := fs.Bool("i", false, "write the entry (needs root)")
	force := fs.Bool("f", false, "overwrite an entry with the same name in "+dm.LocalXSessions+",\nor hide one in another xsessions directory, such as a package's")
	names := fs.String("D", "", "desktop `names` to add, separated by ':'")
	exclusive := fs.Bool("e", false, "use only the names given with -D, dropping the known ones")
	fromTable := fs.Bool("from-table", false, "take the entry from uxsm's table of known desktops instead of\nfrom an installed one; needs --exec or --plain")
	noTable := fs.Bool("no-table", false, "do not let the table fill in what the source does not say: the\nrest is what -D, -N and -C give")
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
	if *fromTable && *noTable {
		return errors.New("--from-table takes the entry from the table and --no-table leaves the table out: use one or the other")
	}
	if *fromTable && dashes {
		return errors.New("--from-table takes the command from uxsm's table; for a command of your own, leave it out")
	}
	if *fromTable && !*exec && !*plain {
		return errors.New("--from-table makes the entry from uxsm's table instead of from an installed one, so it needs --exec or --plain")
	}
	// La entrada normal sólo puede salir de la tabla o de un comando, y de cuál
	// se dice, como en todo lo demás.
	if *plain && !dashes && !*fromTable {
		return errors.New("the plain entry cannot come from an installed one: say where it comes from with `--plain --from-table <name>`, or give the command with `--plain -- <command>`")
	}
	if dashes && !*exec && !*plain {
		return errors.New("an entry that points to another entry needs that entry's name; for a command, use --exec or --plain")
	}

	src, err := entrySource(fs.Args(), dashes, *exec, *fromTable, !*noTable)
	if err != nil {
		return err
	}
	opts := sessionentry.Options{Names: *names, Exclusive: *exclusive, Name: *name, Comment: *comment}
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
//
// fromTable es --from-table: la entrada sale de la tabla, y no de una instalada.
// table es lo contrario de --no-table y dice si la tabla puede completar lo que
// la fuente no traiga.
func entrySource(args []string, dashes, exec, fromTable, table bool) (*sessionentry.Source, error) {
	if dashes {
		return sessionentry.FromCommand(args, table)
	}
	id := strings.TrimSuffix(args[0], ".desktop") + ".desktop"
	name := strings.TrimSuffix(id, ".desktop")

	if fromTable {
		return sessionentry.FromTable(name)
	}
	entry, err := desktopentry.Find(desktopentry.XSessions, id)
	if err == nil {
		return sessionentry.FromEntry(entry, table)
	}
	// Sin entrada instalada no hay fuente, y la tabla no se usa sin pedirlo: se
	// dice cómo pedirla, si es que conoce ese escritorio.
	if exec {
		if _, terr := sessionentry.FromTable(name); terr == nil {
			return nil, fmt.Errorf("there is no session entry %s to take the command from; take it from uxsm's table with `uxsm entry --exec --from-table %s`, or give the command with `uxsm entry --exec -- <command>`", id, name)
		}
		return nil, fmt.Errorf("there is no session entry %s to take the command from, and %s is not in uxsm's table of known desktops; give the command with `uxsm entry --exec -- <command>`", id, name)
	}
	// Una entrada de uxsm que apunta a otra necesita que la otra exista. No se
	// crea por su cuenta: se dice cómo seguir.
	if _, terr := sessionentry.FromTable(name); terr == nil {
		return nil, fmt.Errorf("there is no session entry %s to point to; create it first with `uxsm entry --plain --from-table %s`, or make one that starts the command directly with `uxsm entry --exec --from-table %s`", id, name, name)
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
