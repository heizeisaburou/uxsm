package dm

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// dirList es una lista de directorios de sesiones en la configuración de un
// display manager: en qué opción está, qué vale si nadie la pone, y qué
// directorios locales tienen que estar dentro.
type dirList struct {
	section, key, defaults string
	// locals son los directorios locales de esta lista. LightDM tiene una sola
	// para todo, así que lleva los dos; SDDM tiene una por tipo de sesión.
	locals []string
}

// keyfileDM describe un display manager que guarda sus directorios de sesiones
// en opciones de sus ficheros INI, como LightDM y SDDM.
type keyfileDM struct {
	name, id string
	// sep es el separador de sus listas.
	sep string
	// lists son las opciones donde están los directorios.
	lists []dirList
	// dirs son sus directorios de ficheros *.conf, en el orden en que los lee,
	// y main, el fichero principal, que lee el último.
	dirs []string
	main string
	// own es el fichero que crea uxsm setup sessions-dir cuando no puede
	// cambiar el que pone la opción.
	own string
}

// lightdm guarda una sola lista, para las sesiones de X11 y las de Wayland, en
// sessions-directory, separada por ":". Lee los
// ficheros en este orden, comprobado con `lightdm --show-config` en Debian 13:
// los lightdm.conf.d de los directorios de datos XDG (primero /usr/share, que
// es el de menos preferencia), el de /etc/xdg, el de /etc/lightdm y por último
// lightdm.conf. Su valor de serie, compilado en el binario, no incluye
// LocalXSessions.
var lightdm = keyfileDM{
	name: "LightDM", id: "lightdm",
	sep: ":",
	lists: []dirList{{
		section: "LightDM", key: "sessions-directory",
		defaults: "/usr/share/lightdm/sessions:/usr/share/xsessions:/usr/share/wayland-sessions",
		locals:   LocalSessions,
	}},
	dirs: []string{
		"/usr/share/lightdm/lightdm.conf.d",
		"/usr/local/share/lightdm/lightdm.conf.d",
		"/etc/xdg/lightdm/lightdm.conf.d",
		"/etc/lightdm/lightdm.conf.d",
	},
	main: "/etc/lightdm/lightdm.conf",
	own:  "/etc/lightdm/lightdm.conf.d/99-uxsm.conf",
}

// sddm guarda una lista por tipo de sesión: SessionDir en [X11] y
// WaylandSessionDir en [Wayland], separadas por comas. Lee sus ficheros de
// sistema, los locales y por último sddm.conf, según sddm.conf(5), que da
// también los valores de serie, que ya incluyen los directorios locales.
var sddm = keyfileDM{
	name: "SDDM", id: "sddm",
	sep: ",",
	lists: []dirList{
		{
			section: "X11", key: "SessionDir",
			defaults: "/usr/local/share/xsessions,/usr/share/xsessions",
			locals:   []string{LocalXSessions},
		},
		{
			section: "Wayland", key: "WaylandSessionDir",
			defaults: "/usr/local/share/wayland-sessions,/usr/share/wayland-sessions",
			locals:   []string{LocalWaylandSessions},
		},
	},
	dirs: []string{"/usr/lib/sddm/sddm.conf.d", "/etc/sddm.conf.d"},
	main: "/etc/sddm.conf",
	own:  "/etc/sddm.conf.d/99-uxsm.conf",
}

func readLightDM() (*Report, error) { return lightdm.read() }
func readSDDM() (*Report, error)    { return sddm.read() }

// read lee las listas en vigor: de cada opción, la del último fichero que la
// pone o, si ninguno la pone, la de serie.
func (k keyfileDM) read() (*Report, error) {
	r := &Report{Name: k.name, ID: k.id}
	var origins []string
	for _, l := range k.lists {
		s, err := k.lookup(l)
		if err != nil {
			return nil, err
		}
		value, origin := l.defaults, "compiled default"
		if s.file != "" {
			value, origin = s.value, s.file
		}
		r.Dirs = append(r.Dirs, splitList(value, k.sep)...)
		if !slices.Contains(origins, origin) {
			origins = append(origins, origin)
		}
	}
	r.Origin = strings.Join(origins, ", ")
	return r, nil
}

func (k keyfileDM) lookup(l dirList) (setting, error) {
	files, err := confFiles(k.dirs, k.main)
	if err != nil {
		return setting{}, err
	}
	return lookup(files, l.section, l.key)
}

// Change es un cambio en un fichero de configuración.
type Change struct {
	// File es el fichero que se escribe.
	File string
	// Old es su contenido actual, "" si no existe; New, el que tendrá.
	Old, New string
}

// Apply escribe el cambio.
func (c *Change) Apply() error {
	p := path(c.File)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(c.New), 0o644)
}

// SetupSessionsDir calcula los cambios para que el display manager id,
// "lightdm" o "sddm", lea los directorios locales de sesiones. Devuelve nil si
// ya los lee todos.
//
// Si un fichero pone la opción, el directorio se añade en ese mismo fichero,
// aunque sea del usuario: otro fichero que la pisara dejaría dos sitios con la
// misma opción, y cambiar el primero no haría nada. La excepción son los
// ficheros de los paquetes, bajo /usr, que la próxima actualización pisaría:
// entonces, o si nadie pone la opción, se escribe el fichero propio de uxsm,
// que se lee después. Como una opción puede venir de un fichero y otra de otro,
// puede salir más de un cambio.
//
// Los directorios locales van delante de todo, como en XDG_DATA_DIRS, donde
// /usr/local/share va antes que /usr/share: así una entrada local tapa a la del
// paquete con el mismo nombre, igual que en todos los demás sitios. Y entre
// ellos, el de Wayland delante del de X11, para no inclinar la máquina hacia
// X11 sólo porque uxsm sea de X11.
func SetupSessionsDir(id string) ([]Change, error) {
	var k keyfileDM
	switch id {
	case "lightdm":
		k = lightdm
	case "sddm":
		k = sddm
	default:
		return nil, fmt.Errorf("uxsm cannot change the session directories of %q", id)
	}

	// Lo que hay que escribir en el fichero propio, si hace falta: puede ser
	// más de una opción, cada una en su grupo.
	var ownLines []string
	var changes []Change

	for _, l := range k.lists {
		s, err := k.lookup(l)
		if err != nil {
			return nil, err
		}
		dirs := splitList(l.defaults, k.sep)
		if s.file != "" {
			dirs = splitList(s.value, k.sep)
		}
		with := addLocals(dirs, l.locals)
		if len(with) == len(dirs) {
			continue // ya los lee todos
		}
		line := l.key + "=" + strings.Join(with, k.sep)

		if s.file != "" && !strings.HasPrefix(s.file, "/usr/") {
			old, err := readFile(s.file)
			if err != nil {
				return nil, err
			}
			changes = append(changes, Change{File: s.file, Old: old, New: replaceLine(old, s.line, line)})
			continue
		}
		ownLines = append(ownLines, "["+l.section+"]", line)
	}

	if len(ownLines) > 0 {
		old, err := readFile(k.own)
		if err != nil {
			return nil, err
		}
		content := "# Written by `uxsm setup sessions-dir " + k.id + "`: adds " +
			strings.Join(LocalSessions, " and ") + ",\n" +
			"# where session entries installed by hand live, uxsm's among them.\n" +
			strings.Join(ownLines, "\n") + "\n"
		changes = append(changes, Change{File: k.own, Old: old, New: content})
	}
	return changes, nil
}

// addLocals pone delante de dirs los directorios locales que falten, en el
// orden de locals. Los que ya estén se quedan donde estén: si alguien los puso
// en otro sitio a propósito, no somos quién para moverlos.
func addLocals(dirs, locals []string) []string {
	var add []string
	for _, local := range locals {
		if !slices.Contains(dirs, local) {
			add = append(add, local)
		}
	}
	return slices.Concat(add, dirs)
}

// readGDM calcula los directorios de xsessions de GDM a partir de cómo lo
// arranca systemd. GDM los busca en el subdirectorio xsessions de cada
// directorio de su XDG_DATA_DIRS, más /usr/share/xsessions, que lleva
// compilado. Ese XDG_DATA_DIRS es el del entorno del gestor del sistema, con
// lo que añadan las líneas Environment= y EnvironmentFile= de su unidad.
//
// No se lee del proceso de GDM: haría falta root y que estuviera en marcha. A
// cambio, no se ve lo que GDM pudiera cambiar por su cuenta después, y por
// eso Origin lo dice.
func readGDM() (*Report, error) {
	unit, err := gdmUnit()
	if err != nil {
		return nil, err
	}
	env, err := gdmEnvironment(unit)
	if err != nil {
		return nil, err
	}
	data := env["XDG_DATA_DIRS"]
	if data == "" {
		data = "/usr/local/share:/usr/share"
	}
	var dirs []string
	for _, d := range splitList(data, ":") {
		dirs = append(dirs, d+"/xsessions")
	}
	if !slices.Contains(dirs, "/usr/share/xsessions") {
		dirs = append(dirs, "/usr/share/xsessions")
	}
	return &Report{Name: "GDM", ID: "gdm", Dirs: dirs, Origin: "as systemd launches " + unit, Unit: unit}, nil
}

// gdmUnit es la unidad de GDM: gdm.service o, en Debian, gdm3.service.
func gdmUnit() (string, error) {
	for _, u := range []string{"gdm.service", "gdm3.service"} {
		out, err := systemctl("show", "-p", "LoadState", u)
		if err != nil {
			return "", fmt.Errorf("asking systemd for %s: %w", u, err)
		}
		if parseProps(out)["LoadState"] == "loaded" {
			return u, nil
		}
	}
	return "", fmt.Errorf("GDM is not installed")
}

// gdmEnvironment es el entorno con el que systemd arranca unit: el del gestor
// del sistema, pisado por Environment= y éste, por EnvironmentFile=.
func gdmEnvironment(unit string) (map[string]string, error) {
	env := map[string]string{}
	out, err := systemctl("show-environment")
	if err != nil {
		return nil, fmt.Errorf("reading the systemd environment: %w", err)
	}
	for _, l := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			env[k] = v
		}
	}

	out, err = systemctl("show", "-p", "Environment", "-p", "EnvironmentFiles", unit)
	if err != nil {
		return nil, fmt.Errorf("asking systemd for %s: %w", unit, err)
	}
	var files []string
	for _, l := range strings.Split(out, "\n") {
		k, v, _ := strings.Cut(l, "=")
		switch k {
		case "Environment":
			for _, a := range splitQuoted(v) {
				if n, val, ok := strings.Cut(a, "="); ok {
					env[n] = val
				}
			}
		case "EnvironmentFiles":
			// "/etc/default/gdm (ignore_errors=yes)"
			if f, _, _ := strings.Cut(v, " ("); f != "" {
				files = append(files, f)
			}
		}
	}
	for _, f := range files {
		text, err := readFile(f)
		if err != nil {
			return nil, err
		}
		for _, l := range strings.Split(text, "\n") {
			l = strings.TrimSpace(l)
			if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, ";") {
				continue
			}
			if n, val, ok := strings.Cut(l, "="); ok {
				env[strings.TrimSpace(n)] = strings.Trim(strings.TrimSpace(val), `"'`)
			}
		}
	}
	return env, nil
}

// splitQuoted parte el valor de Environment= como lo enseña systemctl show:
// asignaciones separadas por espacios, las que llevan espacios entre comillas
// dobles.
func splitQuoted(s string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case r == ' ' && !quoted:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
