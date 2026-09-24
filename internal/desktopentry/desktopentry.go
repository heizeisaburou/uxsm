// Package desktopentry lee entradas de sesión (.desktop) según la Desktop Entry
// Specification: sólo lo que uxsm necesita para arrancar una sesión y para
// generar entradas a partir de otras.
package desktopentry

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/heizeisaburou/uxsm/internal/xdg"
)

// Los subdirectorios de cada directorio de datos XDG donde hay entradas: las de
// sesión X11 y las de aplicación.
const (
	XSessions    = "xsessions"
	Applications = "applications"
)

// Entry es una entrada de sesión ya leída.
type Entry struct {
	// ID es el nombre del fichero, con .desktop: "bspwm.desktop".
	ID string
	// Path es la ruta del fichero que se ha leído.
	Path string
	// Name es la clave Name=, sin traducciones.
	Name string
	// Comment es la clave Comment=, sin traducciones.
	Comment string
	// Exec es la clave Exec= tal cual; para ejecutarla, ver SplitExec.
	Exec string
	// DesktopNames es la lista de DesktopNames=.
	DesktopNames []string
	// Icon es la clave Icon=, que es lo que pone %i en el Exec=.
	Icon string
	// WorkingDir es la clave Path=: el directorio desde el que se ejecuta la
	// aplicación. Se llama así para no confundirlo con Path, que es dónde está
	// la entrada.
	WorkingDir string
	// Terminal dice si la entrada pide ejecutarse dentro de un terminal.
	Terminal bool
	// Actions son las acciones de la entrada, los grupos [Desktop Action X],
	// por su identificador.
	Actions map[string]Action
}

// Action es una acción de una entrada: otra cosa que se puede lanzar desde
// ella, como "abrir una ventana privada".
type Action struct {
	// ID es lo que va detrás de ":" al pedirla: "new-private-window".
	ID string
	// Name es su nombre, y Exec lo que ejecuta.
	Name, Exec string
}

// Find busca la entrada id en el subdirectorio subdir ("xsessions") de cada
// directorio de datos XDG, por orden de preferencia, y devuelve la primera.
//
// Es el mismo orden en que la buscaría un display manager que siga XDG: una
// entrada en ~/.local/share/xsessions tapa a la del sistema con el mismo ID.
func Find(subdir, id string) (*Entry, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}

	for _, dir := range xdg.DataDirs() {
		for _, rel := range idPaths(id) {
			e, err := Read(filepath.Join(dir, subdir, rel))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			// El ID es el que se pidió, aunque el fichero esté en un
			// subdirectorio: es como lo nombra todo el mundo.
			e.ID = id
			return e, nil
		}
	}

	return nil, fmt.Errorf("desktop entry %q not found in any %s directory", id, subdir)
}

// idPaths son las rutas relativas donde puede estar la entrada id, por orden.
//
// Lo normal es un fichero con ese nombre, pero la especificación dice que el ID
// de una aplicación es su ruta dentro del directorio con las barras cambiadas
// por guiones, así que "kde4-konsole.desktop" puede estar en "kde4/konsole.desktop".
func idPaths(id string) []string {
	paths := []string{id}
	for i, c := range id {
		if c != '-' {
			continue
		}
		paths = append(paths, id[:i]+"/"+id[i+1:])
	}
	return paths
}

// Read lee la entrada del fichero path. Su ID es el nombre del fichero.
func Read(path string) (*Entry, error) {
	id := filepath.Base(path)
	if err := checkID(id); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	e, err := parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	e.ID, e.Path = id, path
	return e, nil
}

// checkID comprueba que id es el nombre de un fichero .desktop y nada más: sin
// barras, para que no se pueda salir del directorio de entradas.
func checkID(id string) error {
	if !strings.HasSuffix(id, ".desktop") || id == ".desktop" {
		return fmt.Errorf("%q is not a desktop entry ID: it must end in .desktop", id)
	}
	if strings.ContainsRune(id, '/') {
		return fmt.Errorf("%q is not a desktop entry ID: it contains a slash", id)
	}
	return nil
}

// parse lee el grupo [Desktop Entry], y de los demás grupos, las acciones.
//
// Las claves traducidas (Name[es]=) se ignoran. Exec= tiene que estar: una
// entrada sin él no se puede lanzar.
func parse(r io.Reader) (*Entry, error) {
	var e Entry
	inMain := false
	seenExec := false
	action := ""

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inMain = line == "[Desktop Entry]"
			action = ""
			if id, ok := strings.CutPrefix(strings.TrimSuffix(line, "]"), "[Desktop Action "); ok {
				action = strings.TrimSpace(id)
			}
			continue
		}
		if !inMain && action == "" {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if action != "" {
			a := e.Actions[action]
			a.ID = action
			switch key {
			case "Name":
				a.Name = unescape(value)
			case "Exec":
				a.Exec = value
			}
			if e.Actions == nil {
				e.Actions = map[string]Action{}
			}
			e.Actions[action] = a
			continue
		}

		switch key {
		case "Name":
			e.Name = unescape(value)
		case "Comment":
			e.Comment = unescape(value)
		case "Exec":
			e.Exec = value
			seenExec = true
		case "DesktopNames":
			e.DesktopNames = splitList(value)
		case "Icon":
			e.Icon = unescape(value)
		case "Path":
			e.WorkingDir = unescape(value)
		case "Terminal":
			e.Terminal = value == "true"
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !seenExec || e.Exec == "" {
		return nil, errors.New("no Exec= key in [Desktop Entry]")
	}

	return &e, nil
}

// unescape deshace los escapes de los valores de tipo cadena: \s, \n, \t, \r y \\.
func unescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 's':
			b.WriteByte(' ')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// splitList parte una lista separada por ";", donde "\;" es un ";" literal.
// Los elementos vacíos, como el que deja el ";" final, se descartan.
func splitList(s string) []string {
	var items []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == ';':
			cur.WriteByte(';')
			i++
		case s[i] == ';':
			if cur.Len() > 0 {
				items = append(items, unescape(cur.String()))
			}
			cur.Reset()
		default:
			cur.WriteByte(s[i])
		}
	}
	if cur.Len() > 0 {
		items = append(items, unescape(cur.String()))
	}
	return items
}
