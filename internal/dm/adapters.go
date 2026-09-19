package dm

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// keyfileDM describe un display manager que guarda sus directorios de
// xsessions en una opción de sus ficheros INI, como LightDM y SDDM.
type keyfileDM struct {
	name, id string
	// section y key son la opción; sep, el separador de su lista.
	section, key, sep string
	// dirs son sus directorios de ficheros *.conf, en el orden en que los lee,
	// y main, el fichero principal, que lee el último.
	dirs []string
	main string
	// defaults es el valor que usa si ningún fichero pone la opción.
	defaults string
	// own es el fichero que crea uxsm setup xsessions-dir cuando no puede
	// cambiar el que pone la opción.
	own string
}

// lightdm guarda la lista en sessions-directory, separada por ":". Lee los
// ficheros en este orden, comprobado con `lightdm --show-config` en Debian 13:
// los lightdm.conf.d de los directorios de datos XDG (primero /usr/share, que
// es el de menos preferencia), el de /etc/xdg, el de /etc/lightdm y por último
// lightdm.conf. Su valor de serie, compilado en el binario, no incluye
// LocalXSessions.
var lightdm = keyfileDM{
	name: "LightDM", id: "lightdm",
	section: "LightDM", key: "sessions-directory", sep: ":",
	dirs: []string{
		"/usr/share/lightdm/lightdm.conf.d",
		"/usr/local/share/lightdm/lightdm.conf.d",
		"/etc/xdg/lightdm/lightdm.conf.d",
		"/etc/lightdm/lightdm.conf.d",
	},
	main:     "/etc/lightdm/lightdm.conf",
	defaults: "/usr/share/lightdm/sessions:/usr/share/xsessions:/usr/share/wayland-sessions",
	own:      "/etc/lightdm/lightdm.conf.d/99-uxsm.conf",
}

// sddm guarda la lista en SessionDir, del grupo [X11], separada por comas.
// Lee sus ficheros de sistema, los locales y por último sddm.conf, según
// sddm.conf(5), que da también el valor de serie.
var sddm = keyfileDM{
	name: "SDDM", id: "sddm",
	section: "X11", key: "SessionDir", sep: ",",
	dirs:     []string{"/usr/lib/sddm/sddm.conf.d", "/etc/sddm.conf.d"},
	main:     "/etc/sddm.conf",
	defaults: "/usr/local/share/xsessions,/usr/share/xsessions",
	own:      "/etc/sddm.conf.d/99-uxsm.conf",
}

func readLightDM() (*Report, error) { return lightdm.read() }
func readSDDM() (*Report, error)    { return sddm.read() }

// read lee la lista en vigor: la del último fichero que pone la opción o, si
// ninguno la pone, la de serie.
func (k keyfileDM) read() (*Report, error) {
	s, err := k.lookup()
	if err != nil {
		return nil, err
	}
	r := &Report{Name: k.name, ID: k.id}
	if s.file == "" {
		r.Dirs, r.Origin = splitList(k.defaults, k.sep), "compiled default"
		return r, nil
	}
	r.Dirs, r.Origin = splitList(s.value, k.sep), s.file
	return r, nil
}

func (k keyfileDM) lookup() (setting, error) {
	files, err := confFiles(k.dirs, k.main)
	if err != nil {
		return setting{}, err
	}
	return lookup(files, k.section, k.key)
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

// SetupXSessionsDir calcula el cambio para que el display manager id, "lightdm"
// o "sddm", lea LocalXSessions. Devuelve nil si ya lo lee.
//
// Si un fichero pone la opción, el directorio se añade en ese mismo fichero,
// aunque sea del usuario: otro fichero que la pisara dejaría dos sitios con la
// misma opción, y cambiar el primero no haría nada. La excepción son los
// ficheros de los paquetes, bajo /usr, que la próxima actualización pisaría:
// entonces, o si nadie pone la opción, se crea el fichero propio de uxsm, que
// se lee después.
//
// LocalXSessions va delante de /usr/share/xsessions si está, como en
// XDG_DATA_DIRS y en el valor de serie de SDDM: así una entrada local tapa a
// la del paquete con el mismo nombre, igual que en todos los demás sitios.
func SetupXSessionsDir(id string) (*Change, error) {
	var k keyfileDM
	switch id {
	case "lightdm":
		k = lightdm
	case "sddm":
		k = sddm
	default:
		return nil, fmt.Errorf("uxsm cannot change the xsessions directories of %q", id)
	}
	s, err := k.lookup()
	if err != nil {
		return nil, err
	}

	dirs := splitList(k.defaults, k.sep)
	if s.file != "" {
		dirs = splitList(s.value, k.sep)
	}
	if slices.Contains(dirs, LocalXSessions) {
		return nil, nil
	}
	if i := slices.Index(dirs, "/usr/share/xsessions"); i >= 0 {
		dirs = slices.Insert(dirs, i, LocalXSessions)
	} else {
		dirs = append(dirs, LocalXSessions)
	}
	line := k.key + "=" + strings.Join(dirs, k.sep)

	if s.file != "" && !strings.HasPrefix(s.file, "/usr/") {
		old, err := readFile(s.file)
		if err != nil {
			return nil, err
		}
		return &Change{File: s.file, Old: old, New: replaceLine(old, s.line, line)}, nil
	}
	old, err := readFile(k.own)
	if err != nil {
		return nil, err
	}
	content := "# Written by `uxsm setup xsessions-dir " + k.id + "`: adds " + LocalXSessions + ",\n" +
		"# where uxsm installs the session entries it generates.\n" +
		"[" + k.section + "]\n" + line + "\n"
	return &Change{File: k.own, Old: old, New: content}, nil
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
