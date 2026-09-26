// Package autostart lleva las entradas de autostart XDG a los slices de la
// sesión, que es lo único que el generador de systemd no hace por nosotros.
//
// `systemd-xdg-autostart-generator` crea una `app-<nombre>@autostart.service`
// por entrada y las deja en `app.slice`, el estándar. Para que caigan en
// `app-uxsm.slice`, como todo lo que lanza uxsm, hace falta un añadido sobre la
// plantilla `app-@autostart.service`, que es la que comparten todas. No puede
// venir en el paquete: un añadido en /usr/lib se aplicaría también a las
// sesiones que no son de uxsm ―las de uwsm, las de GNOME― y les cambiaría el
// slice. Así que se escribe al arrancar la sesión y se borra al cerrarla, igual
// que hace uwsm con el suyo.
//
// Quien escriba o borre tiene que pedirle a systemd que recargue después
// (systemd.DaemonReload): los añadidos se leen al cargar la unidad.
package autostart

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/heizeisaburou/uxsm/internal/appunit"
)

// dropInDir es el directorio de añadidos de la plantilla, dentro del directorio
// de unidades de runtime del gestor: lo que se deja ahí no sobrevive al usuario.
const dropInDir = "systemd/user/app-@autostart.service.d"

// dropInName va detrás del "slice-tweak.conf" de uwsm por orden alfabético, que
// es el orden en que systemd aplica los añadidos. Así, si una sesión anterior de
// uwsm dejó el suyo sin borrar, manda el nuestro mientras dura la nuestra.
const dropInName = "uxsm-tweaks.conf"

// dropIn es lo que se escribe. Las dos líneas de [Unit] son las de uwsm y hacen
// que las entradas se puedan parar y arrancar con su target, en vez de sólo con
// la sesión entera.
var dropIn = []byte(`# Escrito por uxsm mientras dura la sesión; se borra al cerrarla.
[Unit]
PartOf=xdg-desktop-autostart.target
After=xdg-desktop-autostart.target

[Service]
Slice=` + appunit.AppSlice + `
`)

// Path es el fichero del añadido.
func Path() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(base) {
		return "", errors.New("XDG_RUNTIME_DIR is not set to an absolute path")
	}
	return filepath.Join(base, dropInDir, dropInName), nil
}

// Write escribe el añadido, creando su directorio si hace falta.
func Write() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, dropIn, 0o644)
}

// Remove borra el añadido, y su directorio si queda vacío. No es un error que no
// esté: se llama al cerrar cualquier sesión, también una que no lo escribió.
func Remove() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// El directorio es de la plantilla, no nuestro: se intenta quitar y si no
	// está vacío ―otro dejó su añadido― se queda, que no es asunto nuestro.
	os.Remove(filepath.Dir(path))
	return nil
}
