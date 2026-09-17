// Package xdg resuelve los directorios de la especificación XDG Base Directory
// que necesita uxsm.
package xdg

import (
	"os"
	"path/filepath"
	"strings"
)

// DataDirs devuelve los directorios de datos por orden de preferencia:
// primero XDG_DATA_HOME y detrás cada uno de XDG_DATA_DIRS.
//
// Si una variable no está o está vacía se usa el valor por defecto de la
// especificación (~/.local/share y /usr/local/share:/usr/share). Las rutas
// relativas se descartan, porque la especificación las declara inválidas.
func DataDirs() []string {
	var dirs []string

	home := os.Getenv("XDG_DATA_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".local", "share")
		}
	}
	if filepath.IsAbs(home) {
		dirs = append(dirs, home)
	}

	system := os.Getenv("XDG_DATA_DIRS")
	if system == "" {
		system = "/usr/local/share:/usr/share"
	}
	for _, d := range strings.Split(system, ":") {
		if filepath.IsAbs(d) {
			dirs = append(dirs, d)
		}
	}

	return dirs
}
