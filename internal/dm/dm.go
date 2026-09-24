// Package dm averigua qué display manager usa el sistema y en qué directorios
// busca las entradas de sesión X11: sus directorios de xsessions.
//
// Hay un adaptador para cada uno de los tres importantes, LightDM, SDDM y GDM,
// y cada uno sabe dónde guarda su display manager esa lista. De cualquier otro
// no se puede saber nada, y el paquete lo dice así en vez de suponer.
//
// Importa porque uxsm instala sus entradas generadas en LocalXSessions, y un
// display manager que no lea ese directorio no las enseña en la pantalla de
// inicio. LightDM, con su configuración de serie, no lo lee.
package dm

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// LocalWaylandSessions es el directorio local de entradas de sesión de Wayland.
// uxsm no genera ninguna ―es cosa de X11―, pero es el sitio donde las pone quien
// las escribe a mano, y el display manager tiene que leerlo por el mismo motivo
// que el de X11: si no, están y no salen.
const LocalWaylandSessions = "/usr/local/share/wayland-sessions"

// LocalSessions son los dos directorios locales que uxsm deja leídos, en el
// orden en que se añaden. Wayland va delante de X11 a propósito: uxsm es de
// X11, pero lo que se arregla es la máquina, no lo nuestro.
var LocalSessions = []string{LocalWaylandSessions, LocalXSessions}

// LocalXSessions es el directorio donde uxsm instala las entradas de sesión
// que genera: el de las entradas locales del sistema, fuera de los paquetes.
const LocalXSessions = "/usr/local/share/xsessions"

// root es la raíz desde la que se leen y escriben los ficheros de
// configuración: "/" salvo en las pruebas, que la cambian por un árbol falso.
var root = "/"

// systemctl ejecuta systemctl contra el gestor del sistema y devuelve su
// salida. Las pruebas lo sustituyen.
var systemctl = func(args ...string) (string, error) {
	out, err := exec.Command("systemctl", args...).Output()
	return string(out), err
}

// path es p dentro de root.
func path(p string) string {
	return filepath.Join(root, p)
}

// Report es lo que se sabe de los directorios de xsessions de un display
// manager.
type Report struct {
	// Name es el nombre para las personas: "LightDM".
	Name string
	// ID es el nombre para uxsm setup sessions-dir: "lightdm".
	ID string
	// Dirs son sus directorios de xsessions, en el orden en que los recorre.
	Dirs []string
	// Origin dice de dónde sale Dirs, para enseñarlo: el fichero que lo pone,
	// "compiled default" o "as systemd launches it".
	Origin string
	// Unit es su unidad de systemd, si hace falta nombrarla: la de GDM, que
	// cambia con la distribución.
	Unit string
}

// Reads dice si el display manager busca entradas en dir.
func (r *Report) Reads(dir string) bool {
	return slices.Contains(r.Dirs, strings.TrimSuffix(dir, "/"))
}

// Missing son los directorios locales que el display manager no lee.
func (r *Report) Missing() []string {
	var missing []string
	for _, dir := range LocalSessions {
		if !r.Reads(dir) {
			missing = append(missing, dir)
		}
	}
	return missing
}

// adapter sabe leer los directorios de xsessions de un display manager.
type adapter struct {
	name  string
	id    string
	units []string
	read  func() (*Report, error)
}

var adapters = []adapter{
	{"LightDM", "lightdm", []string{"lightdm.service"}, readLightDM},
	{"SDDM", "sddm", []string{"sddm.service"}, readSDDM},
	{"GDM", "gdm", []string{"gdm.service", "gdm3.service"}, readGDM},
}

// ErrNoDisplayManager es el error de Active cuando no hay ningún display
// manager activado: se arranca la sesión desde una consola, con startx.
var ErrNoDisplayManager = errors.New("no display manager is enabled (display-manager.service does not exist)")

// UnknownError es el error de Active con un display manager para el que no
// hay adaptador.
type UnknownError struct {
	Unit string
}

func (e *UnknownError) Error() string {
	return fmt.Sprintf("the display manager %s is not one uxsm knows (LightDM, SDDM or GDM), so its session directories cannot be determined", e.Unit)
}

// Active devuelve los directorios de xsessions del display manager que arranca
// el sistema: el de display-manager.service.
func Active() (*Report, error) {
	out, err := systemctl("show", "-p", "Id", "-p", "LoadState", "display-manager.service")
	if err != nil {
		return nil, fmt.Errorf("asking systemd for display-manager.service: %w", err)
	}
	props := parseProps(out)
	if props["LoadState"] != "loaded" {
		return nil, ErrNoDisplayManager
	}
	for _, a := range adapters {
		if slices.Contains(a.units, props["Id"]) {
			return a.read()
		}
	}
	return nil, &UnknownError{Unit: props["Id"]}
}

// Get devuelve los directorios de xsessions del display manager id ("lightdm",
// "sddm" o "gdm"), esté en uso o no. Falla si no está instalado.
func Get(id string) (*Report, error) {
	for _, a := range adapters {
		if a.id != id {
			continue
		}
		installed, err := a.installed()
		if err != nil {
			return nil, err
		}
		if !installed {
			return nil, fmt.Errorf("%s is not installed", a.name)
		}
		return a.read()
	}
	return nil, fmt.Errorf("unknown display manager %q: use lightdm, sddm or gdm", id)
}

// installed dice si alguna de las unidades del display manager existe.
func (a adapter) installed() (bool, error) {
	for _, u := range a.units {
		out, err := systemctl("show", "-p", "LoadState", u)
		if err != nil {
			return false, fmt.Errorf("asking systemd for %s: %w", u, err)
		}
		if parseProps(out)["LoadState"] == "loaded" {
			return true, nil
		}
	}
	return false, nil
}

// parseProps lee la salida de `systemctl show -p …`: una línea NOMBRE=valor
// por propiedad.
func parseProps(out string) map[string]string {
	props := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			props[k] = v
		}
	}
	return props
}

// readFile lee p dentro de root; un fichero que no existe cuenta como vacío.
func readFile(p string) (string, error) {
	data, err := os.ReadFile(path(p))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}

// confFiles devuelve, en orden, los ficheros de configuración de un display
// manager: los *.conf de cada directorio de dirs, ordenados por nombre, y
// detrás el fichero principal. El último que ponga una opción es el que manda.
func confFiles(dirs []string, main string) ([]string, error) {
	var files []string
	for _, d := range dirs {
		matches, err := filepath.Glob(filepath.Join(path(d), "*.conf"))
		if err != nil {
			return nil, err
		}
		slices.Sort(matches)
		for _, m := range matches {
			files = append(files, filepath.Join("/", strings.TrimPrefix(m, filepath.Clean(root))))
		}
	}
	return append(files, main), nil
}
