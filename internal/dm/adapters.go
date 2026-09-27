package dm

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// dirList describes a session-directory list in a display manager's
// configuration: which option holds it, its default, and which local
// directories it must contain.
type dirList struct {
	section, key, defaults string
	// locals contains the local directories for this list. LightDM has one list
	// for everything, so it contains both; SDDM has one per session type.
	locals []string
}

// keyfileDM describes a display manager that stores session directories in INI
// file options, such as LightDM and SDDM.
type keyfileDM struct {
	name, id string
	// sep is its list separator.
	sep string
	// lists contains the options that hold the directories.
	lists []dirList
	// dirs contains its *.conf directories in read order, while main is the main
	// file, read last.
	dirs []string
	main string
	// own is the file created by uxsm setup sessions-dir when it cannot modify
	// the file that sets the option.
	own string
	// unit is its systemd unit, used to explain how to restart it.
	unit string
}

// lightdm stores one colon-separated sessions-directory list for both X11 and
// Wayland sessions. It reads files in this order, verified with
// `lightdm --show-config` on Debian 13: lightdm.conf.d under the XDG data
// directories (/usr/share first because it has the lowest priority), then the
// directories under /etc/xdg and /etc/lightdm, and finally lightdm.conf. Its
// compiled-in default does not include LocalXSessions.
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
	unit: "lightdm.service",
}

// sddm stores one comma-separated list per session type: SessionDir under [X11]
// and WaylandSessionDir under [Wayland]. According to sddm.conf(5), it reads
// system files, local files, and finally sddm.conf; the same manual lists the
// defaults, which already include the local directories.
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
	unit: "sddm.service",
}

func readLightDM() (*Report, error) { return lightdm.read() }
func readSDDM() (*Report, error)    { return sddm.read() }

// read reads the effective lists: for each option, the value from the last file
// that sets it, or the default if no file does.
func (k keyfileDM) read() (*Report, error) {
	r := &Report{Name: k.name, ID: k.id, Unit: k.unit}
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

// Change is a configuration-file change.
type Change struct {
	// File is the file to write.
	File string
	// Old is its current content, or "" if absent; New is its resulting content.
	Old, New string
}

// Apply writes the change.
func (c *Change) Apply() error {
	p := path(c.File)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(c.New), 0o644)
}

// SetupSessionsDir computes the changes needed for display manager id,
// "lightdm" or "sddm", to read the local session directories. It returns nil
// if all are already read.
//
// If a file sets the option, the directory is added to that same file even if
// it belongs to the user: writing the option again in another file would leave
// two definitions, and changing the earlier one would have no effect. Package
// files under /usr are the exception because an upgrade would overwrite them;
// in that case, or when no file sets the option, uxsm writes its own file, which
// is read later. Because different options may come from different files, more
// than one change may be required.
//
// Local directories precede everything else, as in XDG_DATA_DIRS. Because
// /usr/local/share precedes /usr/share in that search order, a local entry can
// shadow a package entry with the same name, consistently with other lookups.
// Among the local directories, Wayland precedes X11 so the machine is not
// biased toward X11 merely because uxsm itself is for X11.
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

	// Content to write to the dedicated file if needed: it may contain more
	// than one option, each in its own group.
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
			continue // it already reads all of them
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

// addLocals prepends missing local directories to dirs in locals order.
// Existing ones remain where they are: if someone deliberately placed them
// elsewhere, it is not our place to move them.
func addLocals(dirs, locals []string) []string {
	var add []string
	for _, local := range locals {
		if !slices.Contains(dirs, local) {
			add = append(add, local)
		}
	}
	return slices.Concat(add, dirs)
}

// readGDM computes GDM's xsessions directories from the way systemd starts it.
// GDM searches the xsessions subdirectory of each XDG_DATA_DIRS directory plus
// its compiled-in /usr/share/xsessions directory. XDG_DATA_DIRS comes from the
// system manager's environment with additions from the unit's Environment= and
// EnvironmentFile= lines.
//
// The environment is not read from GDM's process because that would require
// root and a running GDM. As a tradeoff, changes made later by GDM itself are
// not visible, which is why Origin describes the source.
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

// gdmUnit returns GDM's unit: gdm.service or, on Debian, gdm3.service.
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

// gdmEnvironment is the environment with which systemd starts unit: the system
// manager's environment overridden by Environment=, then by EnvironmentFile=.
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

// splitQuoted splits an Environment= value as displayed by systemctl show:
// space-separated assignments, with assignments containing spaces enclosed in
// double quotes.
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
