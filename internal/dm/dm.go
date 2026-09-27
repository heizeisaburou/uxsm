// Package dm determines which display manager the system uses and which
// directories it searches for X11 session entries: its xsessions directories.
//
// There is an adapter for each of the three major display managers—LightDM,
// SDDM, and GDM—and each knows where that display manager keeps the list. No
// reliable answer is available for other display managers, so the package says
// so instead of guessing.
//
// This matters because uxsm installs generated entries in LocalXSessions, and a
// display manager that does not read that directory will not show them on the
// login screen. LightDM does not read it with its default configuration.
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

// LocalWaylandSessions is the local directory for Wayland session entries. uxsm
// does not generate them—it is for X11—but this is where manually written ones
// belong, and the display manager must read it for the same reason as the X11
// directory: otherwise the entries exist but are not shown.
const LocalWaylandSessions = "/usr/local/share/wayland-sessions"

// LocalSessions contains the two local directories uxsm arranges to be read, in
// insertion order. Wayland deliberately precedes X11: uxsm is for X11, but the
// configuration applies to the machine rather than only to uxsm.
var LocalSessions = []string{LocalWaylandSessions, LocalXSessions}

// LocalXSessions is where uxsm installs generated session entries: the system's
// local entry directory, outside package ownership.
const LocalXSessions = "/usr/local/share/xsessions"

// root is the root from which configuration files are read and written: "/"
// except in tests, which replace it with a synthetic tree.
var root = "/"

// systemctl runs systemctl against the system manager and returns its output.
// Tests replace it.
var systemctl = func(args ...string) (string, error) {
	out, err := exec.Command("systemctl", args...).Output()
	return string(out), err
}

// path is p within root.
func path(p string) string {
	return filepath.Join(root, p)
}

// Report contains what is known about a display manager's xsessions directories.
type Report struct {
	// Name is the human-readable name: "LightDM".
	Name string
	// ID is the name accepted by uxsm setup sessions-dir: "lightdm".
	ID string
	// Dirs contains its xsessions directories in search order.
	Dirs []string
	// Origin says where Dirs came from for reporting purposes: the file that
	// sets it, "compiled default", or "as systemd launches it".
	Origin string
	// Unit is its systemd unit, used when explaining how to restart it; GDM's
	// unit varies by distribution.
	Unit string
}

// Reads says whether the display manager searches dir for entries.
func (r *Report) Reads(dir string) bool {
	return slices.Contains(r.Dirs, strings.TrimSuffix(dir, "/"))
}

// Missing returns the local directories the display manager does not read.
func (r *Report) Missing() []string {
	var missing []string
	for _, dir := range LocalSessions {
		if !r.Reads(dir) {
			missing = append(missing, dir)
		}
	}
	return missing
}

// adapter knows how to read a display manager's xsessions directories.
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

// ErrNoDisplayManager is returned by Active when no display manager is enabled,
// as when the session is started from a console with startx.
var ErrNoDisplayManager = errors.New("no display manager is enabled (display-manager.service does not exist)")

// UnknownError is returned by Active for a display manager with no adapter.
type UnknownError struct {
	Unit string
}

func (e *UnknownError) Error() string {
	return fmt.Sprintf("the display manager %s is not one uxsm knows (LightDM, SDDM or GDM), so its session directories cannot be determined", e.Unit)
}

// Active returns the xsessions directories of the display manager started by
// the system: the one behind display-manager.service.
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

// Get returns the xsessions directories of display manager id ("lightdm",
// "sddm", or "gdm"), whether active or not. It fails if it is not installed.
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

// installed says whether any of the display manager's units exists.
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

// parseProps reads `systemctl show -p …` output: one NAME=value line per property.
func parseProps(out string) map[string]string {
	props := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			props[k] = v
		}
	}
	return props
}

// readFile reads p within root; a missing file counts as empty.
func readFile(p string) (string, error) {
	data, err := os.ReadFile(path(p))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}

// confFiles returns a display manager's configuration files in order: the
// name-sorted *.conf files from each directory in dirs, followed by the main
// file. The last file to set an option takes precedence.
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
