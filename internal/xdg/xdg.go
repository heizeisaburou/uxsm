// Package xdg resolves the XDG Base Directory locations uxsm needs.
package xdg

import (
	"os"
	"path/filepath"
	"strings"
)

// DataDirs returns data directories in preference order: XDG_DATA_HOME first,
// followed by each directory in XDG_DATA_DIRS.
//
// If a variable is unset or empty, the specification's default is used
// (~/.local/share and /usr/local/share:/usr/share). Relative paths are discarded
// because the specification declares them invalid.
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
