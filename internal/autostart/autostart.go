// Package autostart places XDG autostart entries in the session slices, the one
// thing systemd's generator does not do for us.
//
// `systemd-xdg-autostart-generator` creates one
// `app-<name>@autostart.service` per entry and leaves them in the standard
// `app.slice`. Placing them in `app-uxsm.slice`, like everything uxsm launches,
// requires a drop-in for the shared `app-@autostart.service` template. The
// package cannot ship that drop-in: one in /usr/lib would also apply to non-uxsm
// sessions—uwsm and GNOME sessions—and change their slice. It is therefore
// written when the session starts and removed when it ends, as uwsm does.
//
// Whoever writes or removes it must ask systemd to reload afterwards
// (systemd.DaemonReload), because drop-ins are read when the unit is loaded.
package autostart

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/heizeisaburou/uxsm/internal/appunit"
)

// dropInDir is the template's drop-in directory inside the manager's runtime
// unit directory; anything left there does not outlive the user session.
const dropInDir = "systemd/user/app-@autostart.service.d"

// dropInName sorts after uwsm's "slice-tweak.conf", which matters because
// systemd applies drop-ins alphabetically. If a previous uwsm session failed to
// remove its drop-in, ours therefore takes precedence while our session lasts.
const dropInName = "uxsm-tweaks.conf"

// dropIn is the content written to disk. The two [Unit] lines match uwsm and let
// entries start and stop with their target instead of only with the whole
// session.
var dropIn = []byte(`# Escrito por uxsm mientras dura la sesión; se borra al cerrarla.
[Unit]
PartOf=xdg-desktop-autostart.target
After=xdg-desktop-autostart.target

[Service]
Slice=` + appunit.AppSlice + `
`)

// Path is the drop-in file.
func Path() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(base) {
		return "", errors.New("XDG_RUNTIME_DIR is not set to an absolute path")
	}
	return filepath.Join(base, dropInDir, dropInName), nil
}

// Write writes the drop-in, creating its directory if necessary.
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

// Remove deletes the drop-in and its directory if empty. A missing drop-in is
// not an error: this runs whenever any session ends, including one that did not
// write it.
func Remove() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// The directory belongs to the template, not to us: attempt to remove it,
	// but leave it alone if another drop-in keeps it nonempty.
	os.Remove(filepath.Dir(path))
	return nil
}
