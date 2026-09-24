// Package sessionentry genera entradas de sesión X11: las de uxsm, que el
// display manager enseña al lado de las demás, y las entradas normales de los
// escritorios de su tabla que no instalan la suya.
//
// Una entrada sale de una fuente (Source): una entrada que ya existe, un
// escritorio de la tabla de escritorios conocidos (known.go) o un comando. De
// cualquier fuente se pueden generar tres ficheros:
//
//   - La entrada normal, bspwm.desktop con Exec=bspwm (Source.Plain).
//   - La de uxsm que apunta a otra entrada, bspwm-uxsm.desktop con
//     Exec=uxsm start bspwm.desktop (Source.Uxsm). Sólo de una entrada que
//     exista. Es lo que recomienda uwsm para Wayland (su README, «From a
//     display manager»): uxsm start lee de ella el Exec=, que así no hay que
//     copiar en argumentos que algunos display managers no saben entrecomillar.
//   - La de uxsm con el comando directo, bspwm-uxsm.desktop con
//     Exec=uxsm start -D bspwm -- bspwm (Source.UxsmExec), que no necesita
//     ninguna otra entrada.
//
// Las entradas de uxsm llevan TryExec=uxsm, para que el display manager las
// esconda si uxsm no está.
//
// Nunca se genera nada a partir de una entrada o un comando que ya use uxsm
// (se arrancaría a sí mismo), que ya arranque su escritorio como servicio de
// systemd --user, como la de qtile en Arch (habría dos gestores para la misma
// sesión), o que sea una meta-sesión, que arranca el script personal del
// usuario en vez de un escritorio (IsMetaSession).
package sessionentry

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/heizeisaburou/uxsm/internal/desktopentry"
	"github.com/heizeisaburou/uxsm/internal/session"
	"github.com/heizeisaburou/uxsm/internal/systemd"
)

// noAutostartFlag es la opción que se añade al Exec= de las entradas de los
// escritorios que lanzan ellos mismos el autostart XDG (startsOwnAutostart).
const noAutostartFlag = "--no-autostart"

// Suffix termina el ID de toda entrada de uxsm: bspwm.desktop da
// bspwm-uxsm.desktop.
const Suffix = "-uxsm.desktop"

// ErrUsesUxsm es el error con una entrada o un comando que ya arranca uxsm.
var ErrUsesUxsm = errors.New("it already uses uxsm")

// ErrUsesSystemd es el error con una entrada o un comando que ya arranca su
// escritorio como servicio de systemd --user.
var ErrUsesSystemd = errors.New("it already starts its desktop as a systemd user service")

// ErrMetaSession es el error con una meta-sesión: una entrada que no arranca un
// escritorio, sino el script personal del usuario.
var ErrMetaSession = errors.New("it is a meta-session that runs the user's own script, not a desktop")

// ErrUnknown es el error de FromTable con un escritorio que no está en la
// tabla o del que no se conoce la orden.
var ErrUnknown = errors.New("not in uxsm's table of known desktops, or no command known for it")

// ErrNoNames es el error cuando no se sabe ningún nombre del escritorio: no
// hay DesktopNames=, no está en la tabla y no se han dado con -D.
var ErrNoNames = errors.New("no desktop names known")

// ErrDropsNames es el error cuando -e tiraría nombres del escritorio que se
// conocen, sin --force-names.
var ErrDropsNames = errors.New("-e would drop desktop names that are known")

// ErrBadNames es el error con nombres que no se pueden pasar con -D.
var ErrBadNames = errors.New("invalid desktop names")

// metaPrograms son los programas que convierten una entrada en meta-sesión. Se
// mira el programa y no el nombre de la entrada, porque es lo que la hace
// meta-sesión. Son los dos casos que hay entre las cien entradas de Arch,
// Debian, Ubuntu y Fedora (test/xsessions.sh):
//
//   - default: el Exec= de lightdm-xsession.desktop, de LightDM en Debian. No
//     es un programa, sino una palabra que sólo entiende LightDM y que quiere
//     decir «la sesión que tenga configurada el usuario», normalmente
//     ~/.xsession.
//   - xinit-compat: el Exec= de xinit-compat.desktop, de Fedora, un script que
//     ejecuta ~/.xsession, ~/.Xclients o /etc/X11/xinit/Xclients.
//
// No hay forma de saber qué escritorio correrá, y si el script del usuario
// arranca uxsm, la sesión se arrancaría a sí misma. uxsm start no las
// comprueba, como tampoco uwsm.
var metaPrograms = map[string]bool{
	"default":      true,
	"xinit-compat": true,
}

// IsMetaSession dice si la orden argv es la de una meta-sesión.
func IsMetaSession(argv []string) bool {
	return len(argv) > 0 && metaPrograms[filepath.Base(argv[0])]
}

// Source es de dónde sale una entrada generada: un escritorio, su orden y lo
// que se sabe de él.
type Source struct {
	// ID es el de su entrada: "bspwm.desktop".
	ID string
	// Name y Comment son los de su entrada, o los de la tabla.
	Name, Comment string
	// Argv es la orden que arranca el escritorio.
	Argv []string
	// Known son los nombres del escritorio que se conocen: los del
	// DesktopNames= de la entrada o, si no trae, los de la tabla.
	Known []string
	// entryNames son los DesktopNames= de la entrada, que uxsm start leerá de
	// ella; vacío si la fuente no es una entrada.
	entryNames []string
	// entry dice si la fuente es una entrada que existe.
	entry bool
	// origin dice de dónde sale, para el comentario de la entrada generada.
	origin string
}

// FromEntry es la fuente de una entrada que existe. Lo que no traiga lo
// completa la tabla.
func FromEntry(e *desktopentry.Entry) (*Source, error) {
	argv, err := desktopentry.SplitExec(e.Exec)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.ID, err)
	}
	if strings.HasSuffix(e.ID, Suffix) {
		return nil, fmt.Errorf("%s: %w", e.ID, ErrUsesUxsm)
	}
	if err := checkCommand(e.ID, e.Exec, argv); err != nil {
		return nil, err
	}
	k := known[e.ID]
	base := strings.TrimSuffix(e.ID, ".desktop")
	s := &Source{
		ID:         e.ID,
		Name:       first(e.Name, k.Name, base),
		Comment:    first(e.Comment, k.Comment),
		Argv:       argv,
		Known:      e.DesktopNames,
		entryNames: e.DesktopNames,
		entry:      true,
		origin:     e.ID,
	}
	if len(s.Known) == 0 {
		s.Known = k.DesktopNames
	}
	return s, nil
}

// FromTable es la fuente de un escritorio de la tabla, por el nombre de su
// entrada, con .desktop o sin él: "bspwm". Sólo si la tabla conoce su orden.
func FromTable(name string) (*Source, error) {
	id := strings.TrimSuffix(name, ".desktop") + ".desktop"
	k, ok := known[id]
	if !ok || k.Exec == "" {
		return nil, fmt.Errorf("%s: %w", strings.TrimSuffix(id, ".desktop"), ErrUnknown)
	}
	// La orden de la tabla no lleva nada que haya que entrecomillar
	// (TestKnown), así que se parte tal cual.
	argv, err := desktopentry.SplitExec(k.Exec)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", id, err)
	}
	return &Source{
		ID: id, Name: k.Name, Comment: k.Comment, Argv: argv, Known: k.DesktopNames,
		origin: "its table of known desktops (" + id + ")",
	}, nil
}

// FromCommand es la fuente de un comando: la entrada se llama como el programa.
// Si ese nombre está en la tabla, sus nombres y su descripción cuentan como
// conocidos.
func FromCommand(argv []string) (*Source, error) {
	if len(argv) == 0 {
		return nil, errors.New("empty command")
	}
	prog := filepath.Base(argv[0])
	if err := checkCommand(prog, strings.Join(argv, " "), argv); err != nil {
		return nil, err
	}
	id := prog + ".desktop"
	k := known[id]
	return &Source{
		ID: id, Name: first(k.Name, prog), Comment: k.Comment, Argv: argv, Known: k.DesktopNames,
		origin: "the command " + prog,
	}, nil
}

// checkCommand rechaza lo que no se debe envolver: lo que ya usa uxsm, lo que
// ya arranca su escritorio como servicio de systemd y las meta-sesiones. exec
// es la orden como texto, para buscar systemctl dentro de un sh -c.
func checkCommand(what, exec string, argv []string) error {
	if filepath.Base(argv[0]) == "uxsm" {
		return fmt.Errorf("%s: %w", what, ErrUsesUxsm)
	}
	if strings.Contains(exec, "systemctl --user start") {
		return fmt.Errorf("%s: %w", what, ErrUsesSystemd)
	}
	if IsMetaSession(argv) {
		return fmt.Errorf("%s: %w", what, ErrMetaSession)
	}
	return nil
}

// Options son las opciones de uxsm entry que cambian la entrada.
type Options struct {
	// Names son los nombres de -D, separados por ":". Se añaden al final de
	// los conocidos, como en uxsm start.
	Names string
	// Exclusive es -e: sólo cuentan los nombres de -D.
	Exclusive bool
	// ForceNames deja que -e tire nombres conocidos.
	ForceNames bool
	// Name y Comment, si no están vacíos, sustituyen a los de la fuente.
	Name, Comment string
}

// names calcula los nombres del escritorio de la entrada generada: los
// conocidos y detrás los de -D, sin repetidos; o, con -e, sólo los de -D.
func (s *Source) names(o Options) ([]string, error) {
	if o.Names != "" && !session.ValidNames(o.Names) {
		return nil, fmt.Errorf("%w: %q: use letters, digits, '_', '.' and '-', separated by ':'", ErrBadNames, o.Names)
	}
	extra := splitNames(o.Names)
	if o.Exclusive {
		if len(extra) == 0 {
			return nil, fmt.Errorf("%w: -e needs desktop names given with -D", ErrBadNames)
		}
		var dropped []string
		for _, n := range s.Known {
			if !slices.Contains(extra, n) {
				dropped = append(dropped, n)
			}
		}
		if len(dropped) > 0 && !o.ForceNames {
			return nil, fmt.Errorf("%s: %w: %s; add them to -D, or use --force-names to drop them anyway",
				s.ID, ErrDropsNames, strings.Join(dropped, ":"))
		}
		return extra, nil
	}
	names := dedupe(append(slices.Clone(s.Known), extra...))
	if len(names) == 0 {
		return nil, fmt.Errorf("%s: %w: it has no DesktopNames= and is not in uxsm's table; give them with -D",
			s.ID, ErrNoNames)
	}
	if !session.ValidNames(strings.Join(names, ":")) {
		return nil, fmt.Errorf("%w: %q cannot be passed with -D", ErrBadNames, strings.Join(names, ":"))
	}
	return names, nil
}

// Entry es una entrada generada. Render la escribe.
type Entry struct {
	// ID es el nombre de su fichero: "bspwm-uxsm.desktop".
	ID string
	// Name y Comment son los que ve el usuario en el display manager.
	Name, Comment string
	// Exec es la orden, ya entrecomillada como pide el formato; TryExec, el
	// programa sin el que el display manager la esconde.
	Exec, TryExec string
	// DesktopNames es la lista de DesktopNames=.
	DesktopNames []string
	// Source es el ID de la fuente, para X-UXSM-Source=.
	Source string
	// origin dice de dónde sale, para el comentario del fichero.
	origin string
}

// Plain genera la entrada normal del escritorio: la que debería haber instalado
// su paquete, con el mismo ID que la fuente.
func (s *Source) Plain(o Options) (*Entry, error) {
	// Con ese ID la arrancaría uxsm start, que lo usa como instancia.
	if err := systemd.CheckInstance(s.ID); err != nil {
		return nil, err
	}
	names, err := s.names(o)
	if err != nil {
		return nil, err
	}
	return &Entry{
		ID: s.ID, Name: first(o.Name, s.Name), Comment: first(o.Comment, s.Comment),
		Exec: quoteExec(s.Argv), TryExec: s.Argv[0], DesktopNames: names,
		Source: s.ID, origin: s.origin,
	}, nil
}

// Uxsm genera la entrada de uxsm que apunta a la entrada de la fuente:
// `uxsm start bspwm.desktop`. Sólo si la fuente es una entrada que existe.
//
// Los nombres van en el Exec= con -D cuando no los trae la entrada, porque
// uxsm start lee la entrada original, no la generada, y no todos los display
// managers pasan el DesktopNames= de la generada a XDG_CURRENT_DESKTOP. Con -e
// van todos con -e -D, para que uxsm start no añada los de la entrada.
func (s *Source) Uxsm(o Options) (*Entry, error) {
	if !s.entry {
		return nil, fmt.Errorf("%s: no such session entry to point to", s.ID)
	}
	if err := systemd.CheckInstance(s.ID); err != nil {
		return nil, err
	}
	names, err := s.names(o)
	if err != nil {
		return nil, err
	}
	exec := []string{"uxsm", "start"}
	if _, own := startsOwnAutostart(names); own {
		exec = append(exec, noAutostartFlag)
	}
	if o.Exclusive {
		exec = append(exec, "-e", "-D", strings.Join(names, ":"))
	} else {
		var extra []string
		for _, n := range names {
			if !slices.Contains(s.entryNames, n) {
				extra = append(extra, n)
			}
		}
		if len(extra) > 0 {
			exec = append(exec, "-D", strings.Join(extra, ":"))
		}
	}
	return s.uxsmEntry(o, names, strings.Join(append(exec, s.ID), " ")), nil
}

// UxsmExec genera la entrada de uxsm que arranca la orden de la fuente como un
// comando: `uxsm start -D bspwm -- bspwm`. Los nombres van siempre en el
// Exec=, porque no hay entrada de la que uxsm start pueda leerlos.
func (s *Source) UxsmExec(o Options) (*Entry, error) {
	// Con un comando, la instancia de las unidades es el nombre del programa.
	if err := systemd.CheckInstance(filepath.Base(s.Argv[0])); err != nil {
		return nil, err
	}
	names, err := s.names(o)
	if err != nil {
		return nil, err
	}
	exec := []string{"uxsm", "start"}
	if _, own := startsOwnAutostart(names); own {
		exec = append(exec, noAutostartFlag)
	}
	if o.Exclusive {
		exec = append(exec, "-e")
	}
	exec = append(exec, "-D", strings.Join(names, ":"), "--", quoteExec(s.Argv))
	return s.uxsmEntry(o, names, strings.Join(exec, " ")), nil
}

func (s *Source) uxsmEntry(o Options, names []string, exec string) *Entry {
	return &Entry{
		ID:   strings.TrimSuffix(s.ID, ".desktop") + Suffix,
		Name: first(o.Name, s.Name) + " (uxsm)", Comment: first(o.Comment, s.Comment),
		Exec: exec, TryExec: "uxsm", DesktopNames: names,
		Source: s.ID, origin: s.origin,
	}
}

// Render escribe la entrada en el formato de la Desktop Entry Specification.
func (e *Entry) Render() []byte {
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	// Sin punto detrás del nombre: así se copia con dos clics.
	fmt.Fprintf(&b, "# Generated by uxsm from %s\n", e.origin)
	b.WriteString("Type=Application\n")
	fmt.Fprintf(&b, "Name=%s\n", escape(e.Name))
	if e.Comment != "" {
		fmt.Fprintf(&b, "Comment=%s\n", escape(e.Comment))
	}
	// Los escapes generales de las cadenas van por encima del entrecomillado:
	// una barra invertida dentro de unas comillas se escribe doble.
	fmt.Fprintf(&b, "Exec=%s\n", escape(e.Exec))
	fmt.Fprintf(&b, "TryExec=%s\n", escape(e.TryExec))
	b.WriteString("DesktopNames=")
	for _, n := range e.DesktopNames {
		b.WriteString(strings.ReplaceAll(escape(n), ";", `\;`) + ";")
	}
	b.WriteString("\n")
	// Marca las entradas generadas por uxsm, para distinguirlas de las que
	// haya escrito alguien a mano.
	fmt.Fprintf(&b, "X-UXSM-Source=%s\n", escape(e.Source))
	return []byte(b.String())
}

// quoteExec escribe argv como el valor de un Exec=: cada argumento que lleve
// algún carácter reservado va entre comillas dobles, con las comillas, la
// comilla invertida, el dólar y la barra invertida escapados, y los % van
// dobles, porque el % solo empieza un código de campo.
func quoteExec(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		a = strings.ReplaceAll(a, "%", "%%")
		if a == "" || strings.ContainsAny(a, " \t\n\"'\\><~|&;$*?#()`") {
			a = `"` + strings.NewReplacer(`"`, `\"`, "`", "\\`", `$`, `\$`, `\`, `\\`).Replace(a) + `"`
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}

// escape pone los escapes de los valores de tipo cadena: \\, \n, \t y \r.
func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, "\r", `\r`).Replace(s)
}

// first devuelve el primer valor no vacío.
func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// splitNames parte una lista separada por ":" sin dejar elementos vacíos.
func splitNames(s string) []string {
	var names []string
	for _, n := range strings.Split(s, ":") {
		if n != "" {
			names = append(names, n)
		}
	}
	return names
}

// dedupe quita los repetidos y deja cada nombre donde apareció por primera vez.
func dedupe(names []string) []string {
	var out []string
	for _, n := range names {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}
