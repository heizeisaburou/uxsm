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

// XSessions es el subdirectorio de cada directorio de datos XDG donde están las
// entradas de sesión X11: /usr/share/xsessions.
const XSessions = "xsessions"

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
		e, err := Read(filepath.Join(dir, subdir, id))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		return e, err
	}

	return nil, fmt.Errorf("session entry %q not found in any %s directory", id, subdir)
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

// parse lee el grupo [Desktop Entry] y se queda con las claves que usa uxsm.
//
// Las claves traducidas (Name[es]=) y los demás grupos (acciones) se ignoran.
// Exec= tiene que estar: una entrada de sesión sin él no se puede arrancar.
func parse(r io.Reader) (*Entry, error) {
	var e Entry
	inMain := false
	seenExec := false

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inMain = line == "[Desktop Entry]"
			continue
		}
		if !inMain {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

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
