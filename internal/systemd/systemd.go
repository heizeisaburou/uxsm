// Package systemd habla con el gestor de systemd del usuario (systemd --user).
//
// De momento lo hace llamando a systemctl, igual que las pruebas a mano: es lo
// más sencillo de seguir y no necesita un cliente de D-Bus.
package systemd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// DesktopUnit es la unidad que ejecuta el escritorio de una entrada de sesión:
// una instancia de la plantilla uxsm-desktop@.service.
//
// id es el ID de la entrada, el nombre de su fichero con .desktop, tal como
// llega a `uxsm start`. Para /usr/share/xsessions/bspwm.desktop, id es
// "bspwm.desktop" y la unidad, "uxsm-desktop@bspwm.desktop.service".
func DesktopUnit(id string) string {
	return "uxsm-desktop@" + id + ".service"
}

// SessionTarget es el target de la sesión de una entrada:
// "uxsm-session@bspwm.desktop.target" para "bspwm.desktop". Mientras está
// activo, lo está graphical-session.target.
func SessionTarget(id string) string {
	return "uxsm-session@" + id + ".target"
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

// ImportEnvironment copia al gestor las variables names del entorno de este
// proceso, las que estén puestas.
func ImportEnvironment(names ...string) error {
	args := append([]string{"--user", "import-environment"}, names...)
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// Start arranca unit y espera a que systemd termine el arranque.
func Start(unit string) error {
	cmd := exec.Command("systemctl", "--user", "start", unit)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
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
