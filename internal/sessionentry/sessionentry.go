// Package sessionentry generates X11 session entries: uxsm entries displayed
// alongside the others by the display manager, and normal entries for desktops
// in its table that do not install their own.
//
// An entry comes from a Source: an existing entry, a desktop in the known
// desktop table (known.go), or a command. Any source can generate three files:
//
//   - The normal entry, bspwm.desktop with Exec=bspwm (Source.Plain).
//   - A uxsm entry pointing to another entry, bspwm-uxsm.desktop with
//     Exec=uxsm start bspwm.desktop (Source.Uxsm). This requires an existing
//     entry. It is the approach uwsm recommends for Wayland ("From a display
//     manager" in its README): uxsm start reads Exec= from that entry, avoiding
//     arguments that some display managers cannot quote correctly.
//   - A uxsm entry with a direct command, bspwm-uxsm.desktop with
//     Exec=uxsm start -D bspwm -- bspwm (Source.UxsmExec), which needs no other
//     entry.
//
// uxsm entries include TryExec=uxsm so the display manager hides them when uxsm
// is unavailable.
//
// Nothing is generated from an entry or command that already uses uxsm (it
// would start itself), already starts its desktop as a systemd --user service,
// as qtile's Arch entry does (two managers would control the same session), or
// is a meta-session that starts the user's personal script instead of a desktop
// (IsMetaSession).
package sessionentry

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/heizeisaburou/uxsm/internal/desktopentry"
	"github.com/heizeisaburou/uxsm/internal/session"
	"github.com/heizeisaburou/uxsm/internal/systemd"
)

// noAutostartFlag is added to Exec= for entries whose desktops launch XDG
// autostart themselves (startsOwnAutostart).
const noAutostartFlag = "--no-autostart"

// Suffix ends every uxsm entry ID: bspwm.desktop becomes bspwm-uxsm.desktop.
const Suffix = "-uxsm.desktop"

// ErrUsesUxsm is returned for an entry or command that already starts uxsm.
var ErrUsesUxsm = errors.New("it already uses uxsm")

// ErrUsesSystemd is returned for an entry or command that already starts its
// desktop as a systemd --user service.
var ErrUsesSystemd = errors.New("it already starts its desktop as a systemd user service")

// ErrMetaSession is returned for a meta-session: an entry that starts the user's
// personal script rather than a desktop.
var ErrMetaSession = errors.New("it is a meta-session that runs the user's own script, not a desktop")

// ErrUnknown is returned by FromTable for a desktop absent from the table or
// without a known command.
var ErrUnknown = errors.New("not in uxsm's table of known desktops, or no command known for it")

// ErrNoNames is returned when no desktop name is known: DesktopNames= is absent,
// the desktop is not in the table, and no name was passed with -D.
var ErrNoNames = errors.New("no desktop names known")

// ErrBadNames is returned for names that cannot be passed with -D.
var ErrBadNames = errors.New("invalid desktop names")

// metaPrograms are the programs that make an entry a meta-session. The program,
// rather than the entry name, is what makes it a meta-session. These are the two
// cases among the hundred Arch, Debian, Ubuntu, and Fedora entries
// (test/xsessions.sh):
//
//   - default: Exec= from LightDM's lightdm-xsession.desktop on Debian. It is
//     not a program, but a word understood only by LightDM meaning "the session
//     configured by the user", normally the ~/.xsession file.
//   - xinit-compat: Exec= from Fedora's xinit-compat.desktop, a script that runs
//     the ~/.xsession, ~/.Xclients, or /etc/X11/xinit/Xclients file.
//
// There is no way to know which desktop will run, and if the user's script
// starts uxsm, the session would start itself. uxsm start does not reject them,
// nor does uwsm.
var metaPrograms = map[string]bool{
	"default":      true,
	"xinit-compat": true,
}

// IsMetaSession says whether argv is a meta-session command.
func IsMetaSession(argv []string) bool {
	return len(argv) > 0 && metaPrograms[filepath.Base(argv[0])]
}

// Source describes where a generated entry comes from: a desktop, its command,
// and what is known about it.
type Source struct {
	// ID is its entry ID: "bspwm.desktop".
	ID string
	// Name and Comment come from its entry or the table.
	Name, Comment string
	// Argv is the command that starts the desktop.
	Argv []string
	// Known contains the known desktop names: DesktopNames= from the entry or,
	// if absent, names from the table.
	Known []string
	// entryNames contains DesktopNames= from the entry, which uxsm start will
	// read from it; empty if the source is not an entry.
	entryNames []string
	// entry says whether the source is an existing entry.
	entry bool
	// origin describes the source for the generated entry's comment.
	origin string
}

// FromEntry builds a source from an existing entry. With table, missing values
// are filled from the known-desktop table; without it, values absent from the
// entry remain absent.
func FromEntry(e *desktopentry.Entry, table bool) (*Source, error) {
	argv, err := desktopentry.SplitExec(e.Exec)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.ID, err)
	}
	if strings.HasSuffix(e.ID, Suffix) {
		return nil, fmt.Errorf("%s: %w", e.ID, ErrUsesUxsm)
	}
	if err := checkCommand(e.ID, e.Exec, argv); err != nil {
		return nil, err
	}
	var k Known
	if table {
		k = known[e.ID]
	}
	base := strings.TrimSuffix(e.ID, ".desktop")
	s := &Source{
		ID:         e.ID,
		Name:       first(e.Name, k.Name, base),
		Comment:    first(e.Comment, k.Comment),
		Argv:       argv,
		Known:      e.DesktopNames,
		entryNames: e.DesktopNames,
		entry:      true,
		origin:     e.ID,
	}
	if len(s.Known) == 0 {
		s.Known = k.DesktopNames
	}
	return s, nil
}

// FromTable builds a source from a desktop in the table, identified by its entry
// name with or without .desktop, such as "bspwm". It requires a known command.
func FromTable(name string) (*Source, error) {
	id := strings.TrimSuffix(name, ".desktop") + ".desktop"
	k, ok := known[id]
	if !ok || k.Exec == "" {
		return nil, fmt.Errorf("%s: %w", strings.TrimSuffix(id, ".desktop"), ErrUnknown)
	}
	// Table commands contain nothing requiring quotes (TestKnown), so splitting
	// them directly is safe.
	argv, err := desktopentry.SplitExec(k.Exec)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", id, err)
	}
	return &Source{
		ID: id, Name: k.Name, Comment: k.Comment, Argv: argv, Known: k.DesktopNames,
		origin: "its table of known desktops (" + id + ")",
	}, nil
}

// FromCommand builds a source from a command; the entry is named after the
// program. With table, names and a description from a matching table entry are
// treated as known.
func FromCommand(argv []string, table bool) (*Source, error) {
	if len(argv) == 0 {
		return nil, errors.New("empty command")
	}
	prog := filepath.Base(argv[0])
	if err := checkCommand(prog, strings.Join(argv, " "), argv); err != nil {
		return nil, err
	}
	id := prog + ".desktop"
	var k Known
	if table {
		k = known[id]
	}
	return &Source{
		ID: id, Name: first(k.Name, prog), Comment: k.Comment, Argv: argv, Known: k.DesktopNames,
		origin: "the command " + prog,
	}, nil
}

// checkCommand rejects commands that must not be wrapped: commands already
// using uxsm, commands already starting their desktop as a systemd service, and
// meta-sessions. exec is the command as text, used to find systemctl inside
// sh -c.
func checkCommand(what, exec string, argv []string) error {
	if filepath.Base(argv[0]) == "uxsm" {
		return fmt.Errorf("%s: %w", what, ErrUsesUxsm)
	}
	if strings.Contains(exec, "systemctl --user start") {
		return fmt.Errorf("%s: %w", what, ErrUsesSystemd)
	}
	if IsMetaSession(argv) {
		return fmt.Errorf("%s: %w", what, ErrMetaSession)
	}
	return nil
}

// Options contains uxsm entry options that alter the generated entry.
type Options struct {
	// Names contains colon-separated names from -D. They are appended to known
	// names, as in uxsm start.
	Names string
	// Exclusive is -e: only names from -D count, as in uxsm start.
	Exclusive bool
	// Name and Comment replace source values when nonempty.
	Name, Comment string
}

// names computes desktop names for the generated entry: known names followed by
// -D names without duplicates, or only -D names with -e.
func (s *Source) names(o Options) ([]string, error) {
	if o.Names != "" && !session.ValidNames(o.Names) {
		return nil, fmt.Errorf("%w: %q: use letters, digits, '_', '.' and '-', separated by ':'", ErrBadNames, o.Names)
	}
	extra := splitNames(o.Names)
	if o.Exclusive {
		if len(extra) == 0 {
			return nil, fmt.Errorf("%w: -e needs desktop names given with -D", ErrBadNames)
		}
		return extra, nil
	}
	names := dedupe(append(slices.Clone(s.Known), extra...))
	if len(names) == 0 {
		return nil, fmt.Errorf("%s: %w: it has no DesktopNames= and none are known for it; give them with -D",
			s.ID, ErrNoNames)
	}
	if !session.ValidNames(strings.Join(names, ":")) {
		return nil, fmt.Errorf("%w: %q cannot be passed with -D", ErrBadNames, strings.Join(names, ":"))
	}
	return names, nil
}

// Entry is a generated entry. Render writes it.
type Entry struct {
	// ID is its file name: "bspwm-uxsm.desktop".
	ID string
	// Name and Comment are displayed to the user by the display manager.
	Name, Comment string
	// Exec is the command, already quoted as required by the format; TryExec is
	// the program whose absence makes the display manager hide the entry.
	Exec, TryExec string
	// DesktopNames is the DesktopNames= list.
	DesktopNames []string
	// Source is the source ID used for X-UXSM-Source=.
	Source string
	// origin describes where it came from for the file comment.
	origin string
}

// Plain generates the normal desktop entry: the one its package should have
// installed, with the same ID as the source.
func (s *Source) Plain(o Options) (*Entry, error) {
	// uxsm start would launch that ID and use it as the instance.
	if err := systemd.CheckInstance(s.ID); err != nil {
		return nil, err
	}
	names, err := s.names(o)
	if err != nil {
		return nil, err
	}
	return &Entry{
		ID: s.ID, Name: first(o.Name, s.Name), Comment: first(o.Comment, s.Comment),
		Exec: quoteExec(s.Argv), TryExec: s.Argv[0], DesktopNames: names,
		Source: s.ID, origin: s.origin,
	}, nil
}

// Uxsm generates a uxsm entry pointing to the source entry:
// `uxsm start bspwm.desktop`. The source must be an existing entry.
//
// Names are included in Exec= with -D when the source entry lacks them because
// uxsm start reads the original entry, not the generated one, and not all
// display managers pass the generated entry's DesktopNames= to
// XDG_CURRENT_DESKTOP. With -e, every name is passed through -e -D so uxsm
// start does not add names from the source entry.
func (s *Source) Uxsm(o Options) (*Entry, error) {
	if !s.entry {
		return nil, fmt.Errorf("%s: no such session entry to point to", s.ID)
	}
	if err := systemd.CheckInstance(s.ID); err != nil {
		return nil, err
	}
	names, err := s.names(o)
	if err != nil {
		return nil, err
	}
	exec := []string{"uxsm", "start"}
	if _, own := startsOwnAutostart(names); own {
		exec = append(exec, noAutostartFlag)
	}
	if o.Exclusive {
		exec = append(exec, "-e", "-D", strings.Join(names, ":"))
	} else {
		var extra []string
		for _, n := range names {
			if !slices.Contains(s.entryNames, n) {
				extra = append(extra, n)
			}
		}
		if len(extra) > 0 {
			exec = append(exec, "-D", strings.Join(extra, ":"))
		}
	}
	return s.uxsmEntry(o, names, strings.Join(append(exec, s.ID), " ")), nil
}

// UxsmExec generates a uxsm entry that starts the source command directly:
// `uxsm start -D bspwm -- bspwm`. Names are always included in Exec= because
// uxsm start has no entry from which to read them.
func (s *Source) UxsmExec(o Options) (*Entry, error) {
	// With a command, the unit instance is the program name.
	if err := systemd.CheckInstance(filepath.Base(s.Argv[0])); err != nil {
		return nil, err
	}
	names, err := s.names(o)
	if err != nil {
		return nil, err
	}
	exec := []string{"uxsm", "start"}
	if _, own := startsOwnAutostart(names); own {
		exec = append(exec, noAutostartFlag)
	}
	if o.Exclusive {
		exec = append(exec, "-e")
	}
	exec = append(exec, "-D", strings.Join(names, ":"), "--", quoteExec(s.Argv))
	return s.uxsmEntry(o, names, strings.Join(exec, " ")), nil
}

func (s *Source) uxsmEntry(o Options, names []string, exec string) *Entry {
	return &Entry{
		ID:   strings.TrimSuffix(s.ID, ".desktop") + Suffix,
		Name: first(o.Name, s.Name) + " (uxsm)", Comment: first(o.Comment, s.Comment),
		Exec: exec, TryExec: "uxsm", DesktopNames: names,
		Source: s.ID, origin: s.origin,
	}
}

// Render writes the entry in Desktop Entry Specification format.
func (e *Entry) Render() []byte {
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	// No period after the name, so it can be copied with a double click.
	fmt.Fprintf(&b, "# Generated by uxsm from %s\n", e.origin)
	b.WriteString("Type=Application\n")
	fmt.Fprintf(&b, "Name=%s\n", escape(e.Name))
	if e.Comment != "" {
		fmt.Fprintf(&b, "Comment=%s\n", escape(e.Comment))
	}
	// General string escaping applies on top of quoting: a backslash inside
	// quotes is written twice.
	fmt.Fprintf(&b, "Exec=%s\n", escape(e.Exec))
	fmt.Fprintf(&b, "TryExec=%s\n", escape(e.TryExec))
	b.WriteString("DesktopNames=")
	for _, n := range e.DesktopNames {
		b.WriteString(strings.ReplaceAll(escape(n), ";", `\;`) + ";")
	}
	b.WriteString("\n")
	// Mark entries generated by uxsm so they can be distinguished from manually
	// written ones.
	fmt.Fprintf(&b, "X-UXSM-Source=%s\n", escape(e.Source))
	return []byte(b.String())
}

// quoteExec writes argv as an Exec= value: each argument containing a reserved
// character is enclosed in double quotes, with double quotes, backticks, dollar
// signs, and backslashes escaped. Percent signs are doubled because a lone %
// starts a field code.
func quoteExec(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		a = strings.ReplaceAll(a, "%", "%%")
		if a == "" || strings.ContainsAny(a, " \t\n\"'\\><~|&;$*?#()`") {
			a = `"` + strings.NewReplacer(`"`, `\"`, "`", "\\`", `$`, `\$`, `\`, `\\`).Replace(a) + `"`
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}

// escape applies string-value escapes: \\, \n, \t, and \r.
func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\t", `\t`, "\r", `\r`).Replace(s)
}

// first returns the first nonempty value.
func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// splitNames splits a colon-separated list without retaining empty elements.
func splitNames(s string) []string {
	var names []string
	for _, n := range strings.Split(s, ":") {
		if n != "" {
			names = append(names, n)
		}
	}
	return names
}

// dedupe removes duplicates and keeps each name at its first occurrence.
func dedupe(names []string) []string {
	var out []string
	for _, n := range names {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}
