// Package desktopentry reads session entries (.desktop) according to the
// Desktop Entry Specification, limited to what uxsm needs to start a session
// and generate entries from existing ones.
package desktopentry

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/heizeisaburou/uxsm/internal/xdg"
)

// Subdirectories of each XDG data directory that contain entries: X11 sessions
// and applications.
const (
	XSessions    = "xsessions"
	Applications = "applications"
)

// Entry is a parsed session entry.
type Entry struct {
	// ID is the file name including .desktop: "bspwm.desktop".
	ID string
	// Path is the path of the file that was read.
	Path string
	// Name is the untranslated Name= key.
	Name string
	// Comment is the untranslated Comment= key.
	Comment string
	// Exec is the raw Exec= key; see SplitExec for execution.
	Exec string
	// DesktopNames is the DesktopNames= list.
	DesktopNames []string
	// Icon is the Icon= key, which supplies %i in Exec=.
	Icon string
	// WorkingDir is the Path= key: the directory from which the application is
	// run. It has a different name to avoid confusion with Path, the entry's
	// own location.
	WorkingDir string
	// Terminal says whether the entry requests execution in a terminal.
	Terminal bool
	// Actions maps entry actions, the [Desktop Action X] groups, by identifier.
	Actions map[string]Action
}

// Action is an entry action: another operation that can be launched from the
// entry, such as "open a private window".
type Action struct {
	// ID is what follows ":" when requesting it: "new-private-window".
	ID string
	// Name is its name, and Exec is what it runs.
	Name, Exec string
}

// Find looks for entry id in subdir ("xsessions") under each XDG data
// directory in preference order, and returns the first match.
//
// This is the order an XDG-compliant display manager would use: an entry in
// ~/.local/share/xsessions shadows a system entry with the same ID.
func Find(subdir, id string) (*Entry, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}

	for _, dir := range xdg.DataDirs() {
		for _, rel := range idPaths(id) {
			e, err := Read(filepath.Join(dir, subdir, rel))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			// The ID is the requested one even when the file is in a
			// subdirectory: that is the name used everywhere else.
			e.ID = id
			return e, nil
		}
	}

	return nil, fmt.Errorf("desktop entry %q not found in any %s directory", id, subdir)
}

// idPaths returns the relative paths where entry id may be found, in order.
//
// Usually it is a file with that name, but the specification defines an
// application's ID as its path within the directory with slashes replaced by
// dashes, so "kde4-konsole.desktop" may be stored as "kde4/konsole.desktop".
func idPaths(id string) []string {
	paths := []string{id}
	for i, c := range id {
		if c != '-' {
			continue
		}
		paths = append(paths, id[:i]+"/"+id[i+1:])
	}
	return paths
}

// Read reads the entry from path. Its ID is the file name.
func Read(path string) (*Entry, error) {
	id := filepath.Base(path)
	if err := checkID(id); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	e, err := parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	e.ID, e.Path = id, path
	return e, nil
}

// checkID verifies that id is only the name of a .desktop file, without slashes,
// so it cannot escape the entry directory.
func checkID(id string) error {
	if !strings.HasSuffix(id, ".desktop") || id == ".desktop" {
		return fmt.Errorf("%q is not a desktop entry ID: it must end in .desktop", id)
	}
	if strings.ContainsRune(id, '/') {
		return fmt.Errorf("%q is not a desktop entry ID: it contains a slash", id)
	}
	return nil
}

// parse reads the [Desktop Entry] group and actions from the other groups.
//
// Localized keys (Name[es]=) are ignored. Exec= is required because an entry
// without it cannot be launched.
func parse(r io.Reader) (*Entry, error) {
	var e Entry
	inMain := false
	seenExec := false
	action := ""

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inMain = line == "[Desktop Entry]"
			action = ""
			if id, ok := strings.CutPrefix(strings.TrimSuffix(line, "]"), "[Desktop Action "); ok {
				action = strings.TrimSpace(id)
			}
			continue
		}
		if !inMain && action == "" {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if action != "" {
			a := e.Actions[action]
			a.ID = action
			switch key {
			case "Name":
				a.Name = unescape(value)
			case "Exec":
				a.Exec = value
			}
			if e.Actions == nil {
				e.Actions = map[string]Action{}
			}
			e.Actions[action] = a
			continue
		}

		switch key {
		case "Name":
			e.Name = unescape(value)
		case "Comment":
			e.Comment = unescape(value)
		case "Exec":
			e.Exec = value
			seenExec = true
		case "DesktopNames":
			e.DesktopNames = splitList(value)
		case "Icon":
			e.Icon = unescape(value)
		case "Path":
			e.WorkingDir = unescape(value)
		case "Terminal":
			e.Terminal = value == "true"
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !seenExec || e.Exec == "" {
		return nil, errors.New("no Exec= key in [Desktop Entry]")
	}

	return &e, nil
}

// unescape decodes escapes in string values: \s, \n, \t, \r, and \\.
func unescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 's':
			b.WriteByte(' ')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// splitList splits a semicolon-separated list where "\;" is a literal ";".
// Empty elements, including the one left by a trailing ";", are discarded.
func splitList(s string) []string {
	var items []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == ';':
			cur.WriteByte(';')
			i++
		case s[i] == ';':
			if cur.Len() > 0 {
				items = append(items, unescape(cur.String()))
			}
			cur.Reset()
		default:
			cur.WriteByte(s[i])
		}
	}
	if cur.Len() > 0 {
		items = append(items, unescape(cur.String()))
	}
	return items
}
