// Package systemd habla con el gestor de systemd del usuario (systemd --user).
//
// De momento lo hace llamando a systemctl y a busctl, igual que las pruebas a
// mano: es lo más sencillo de seguir y no necesita un cliente de D-Bus.
package systemd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// DesktopUnit es la unidad que ejecuta el escritorio de una entrada de sesión:
// una instancia de la plantilla uxsm-desktop@.service.
//
// id es el ID de la entrada, el nombre de su fichero con .desktop, tal como
// llega a `uxsm start`: para /usr/share/xsessions/bspwm.desktop, id es
// "bspwm.desktop" y la unidad, "uxsm-desktop@bspwm.desktop.service". Si la
// sesión se arranca con un comando (`uxsm start -- bspwm`), id es el nombre del
// programa: "uxsm-desktop@bspwm.service".
func DesktopUnit(id string) string {
	return "uxsm-desktop@" + id + ".service"
}

// SessionTarget es el target de la sesión de una entrada:
// "uxsm-session@bspwm.desktop.target" para "bspwm.desktop". Mientras está
// activo, lo está graphical-session.target.
func SessionTarget(id string) string {
	return "uxsm-session@" + id + ".target"
}

// AutostartTarget es el target del autostart XDG de una sesión:
// "uxsm-autostart@bspwm.desktop.target". Al arrancarlo arrastra el target
// estándar de systemd, que es lo único que lo puede arrancar: ese target tiene
// RefuseManualStart=.
func AutostartTarget(id string) string {
	return "uxsm-autostart@" + id + ".target"
}

// BindPIDUnit es la unidad que vigila el proceso pid de la sesión y la apaga
// cuando termina: "uxsm-bindpid@1234.service".
func BindPIDUnit(pid int) string {
	return "uxsm-bindpid@" + strconv.Itoa(pid) + ".service"
}

// ShutdownTarget es el target que apaga la sesión: al arrancarlo, systemd para
// todo lo que choca con él (Conflicts=).
const ShutdownTarget = "uxsm-shutdown.target"

// CheckInstance comprueba que id vale tal cual como instancia de una unidad.
//
// systemd sólo admite letras, números y ":", "-", "_", "." en los nombres de
// unidad. Un ID con otros caracteres habría que escaparlo (systemd-escape); de
// momento se rechaza, porque las entradas de sesión reales no los usan.
func CheckInstance(id string) error {
	for _, r := range id {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune(":-_.", r)
		if !ok {
			return fmt.Errorf("%q cannot be used as a unit instance: character %q is not allowed", id, r)
		}
	}
	return nil
}

// Environment devuelve el entorno del gestor, en formato "NOMBRE=valor".
//
// Lo pide por D-Bus con busctl, que da cada variable tal cual en JSON. No se usa
// `systemctl show-environment` porque escapa algunos valores como $'…' y habría
// que deshacerlo a mano.
func Environment() ([]string, error) {
	out, err := exec.Command("busctl", "--user", "--json=short", "get-property",
		"org.freedesktop.systemd1", "/org/freedesktop/systemd1",
		"org.freedesktop.systemd1.Manager", "Environment").Output()
	if err != nil {
		return nil, fmt.Errorf("reading the systemd user environment: %w", err)
	}
	var reply struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(out, &reply); err != nil {
		return nil, fmt.Errorf("parsing the systemd user environment: %w", err)
	}
	return reply.Data, nil
}

// UnsetEnvironment borra del gestor las variables names.
func UnsetEnvironment(names ...string) error {
	if len(names) == 0 {
		return nil
	}
	args := append([]string{"--user", "unset-environment"}, names...)
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// DBusIsBroker indica si el bus de sesión usa dbus-broker.
//
// UXSM necesita saberlo porque systemd y D-Bus pueden mantener entornos de
// activación distintos.
//
// Con dbus-broker, la activación de servicios se delega en systemd. Es
// `systemd --user` quien termina ejecutando el proceso, así que éste recibe
// directamente el entorno del gestor y no hace falta mantener otro aparte.
//
// Con dbus-daemon, D-Bus puede ejecutar el servicio por su cuenta. En ese caso
// usa su propio entorno de activación, independiente del de systemd, y UXSM
// tiene que actualizarlo también mediante UpdateDBusActivationEnvironment.
//
// "Puede" porque depende de la activación de cada servicio: si su fichero
// D-Bus declara SystemdService= y dbus-daemon tiene habilitada la activación
// mediante systemd, delega el arranque en `systemd --user`. En caso contrario,
// dbus-daemon ejecuta directamente el Exec= del fichero D-Bus.
//
// Igual que uwsm, se distingue entre ambos comprobando a qué unidad resuelve
// `dbus.service`.
func DBusIsBroker() bool {
	out, err := exec.Command("systemctl", "--user", "show", "-p", "Id", "--value", "dbus.service").Output()
	return err == nil && strings.TrimSpace(string(out)) == "dbus-broker.service"
}

// UpdateDBusActivationEnvironment pone vars, en formato "NOMBRE=valor", en el
// entorno de activación de dbus-daemon. D-Bus no permite borrar variables: para
// «borrar» una se le pone el valor vacío.
func UpdateDBusActivationEnvironment(vars []string) error {
	if len(vars) == 0 {
		return nil
	}
	args := []string{"--user", "call", "org.freedesktop.DBus", "/org/freedesktop/DBus",
		"org.freedesktop.DBus", "UpdateActivationEnvironment", "a{ss}", strconv.Itoa(len(vars))}
	for _, kv := range vars {
		name, value, _ := strings.Cut(kv, "=")
		args = append(args, name, value)
	}
	cmd := exec.Command("busctl", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// SetEnvironment pone en el gestor las variables vars, en formato
// "NOMBRE=valor". Los valores llegan a systemctl como argumentos, sin pasar por
// una shell, así que pueden llevar espacios o comillas sin escapar nada.
func SetEnvironment(vars ...string) error {
	if len(vars) == 0 {
		return nil
	}
	args := append([]string{"--user", "set-environment"}, vars...)
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// DaemonReload le pide al gestor que vuelva a leer sus unidades. Hace falta
// después de escribir o borrar un añadido en el directorio de unidades de
// runtime: los añadidos se leen al cargar la unidad, no al arrancarla.
func DaemonReload() error {
	return systemctl("daemon-reload")
}

// LiveUnits devuelve las unidades que coinciden con patterns y que aún no han
// terminado: están activas, arrancando, recargando o apagándose.
//
// Una unidad en `deactivating` sigue contando como viva hasta que systemd ha
// terminado todo su apagado, incluidos los ExecStopPost=.
func LiveUnits(patterns ...string) ([]string, error) {
	args := append([]string{"--user", "list-units", "--all", "--plain", "--no-legend",
		"--state=active,activating,deactivating,reloading"}, patterns...)
	out, err := exec.Command("systemctl", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("listing systemd user units: %w", err)
	}
	var units []string
	for _, line := range strings.Split(string(out), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			units = append(units, fields[0])
		}
	}
	return units, nil
}

// Start arranca unit y espera a que systemd termine el arranque.
func Start(unit string) error {
	return systemctl("start", unit)
}

// StartNoBlock arranca unit sin esperar a que systemd acabe el trabajo. Es lo
// que hay que usar desde dentro de otra unidad, como hace el ExecStartPost= del
// escritorio: esperar allí a un trabajo de systemd puede dejar a los dos
// esperándose.
func StartNoBlock(unit string) error {
	return systemctl("start", "--no-block", unit)
}

// systemctl ejecuta una orden de systemctl sobre el gestor del usuario y deja
// lo que diga en la salida de uxsm.
func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// CheckUserBus comprueba que existe el bus de sesión de D-Bus. Lo necesitan
// busctl y ExecStartWait: `systemctl --user start --wait` espera por D-Bus a que
// la unidad termine, y sin bus falla con un mensaje que no dice qué falta
// ("Failed to connect to user scope bus via local transport"). Las demás
// órdenes de systemctl funcionan sin él, a través del socket privado del gestor.
//
// Busca el bus donde lo busca systemctl: en la ruta de DBUS_SESSION_BUS_ADDRESS
// si es unix:path=…, o en $XDG_RUNTIME_DIR/bus si no está puesta. Con otra
// dirección no lo puede comprobar y la da por buena.
func CheckUserBus() error {
	path := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "bus")
	if addr := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); addr != "" {
		rest, ok := strings.CutPrefix(addr, "unix:path=")
		if !ok {
			return nil
		}
		path, _, _ = strings.Cut(rest, ",")
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Type() != os.ModeSocket {
		return fmt.Errorf("no D-Bus session bus at %s: uxsm needs the session bus of systemd --user "+
			"(on Debian and Ubuntu, the dbus-user-session package)", path)
	}
	return nil
}

// ExecStartWait reemplaza el proceso actual por:
//
//	systemctl --user start --wait unit
//
// `syscall.Exec` conserva el PID, así que el display manager sigue vigilando
// el mismo proceso, que ahora es `systemctl`. Este espera hasta que la unidad
// termine; cuando eso ocurre, también termina el proceso de sesión.
//
// Si `exec` funciona, esta función no retorna. Sólo devuelve un error si no
// puede ejecutar `systemctl`.
func ExecStartWait(unit string) error {
	path, err := exec.LookPath("systemctl")
	if err != nil {
		return err
	}
	argv := []string{"systemctl", "--user", "start", "--wait", unit}
	return syscall.Exec(path, argv, os.Environ())
}
