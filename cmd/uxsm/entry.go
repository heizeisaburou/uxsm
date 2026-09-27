package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/heizeisaburou/uxsm/internal/desktopentry"
	"github.com/heizeisaburou/uxsm/internal/dm"
	"github.com/heizeisaburou/uxsm/internal/sessionentry"
	"github.com/heizeisaburou/uxsm/internal/xdg"
)

// runEntry generates a session entry and installs it in dm.LocalXSessions:
//
//	uxsm entry [options] bspwm                  bspwm-uxsm.desktop, pointing to bspwm.desktop
//	uxsm entry [options] --exec bspwm           bspwm-uxsm.desktop, with bspwm's command
//	uxsm entry [options] --exec -- mywm [args]  mywm-uxsm.desktop, with that command
//	uxsm entry [options] --plain --from-table bspwm  bspwm.desktop, from the table
//	uxsm entry [options] --plain -- mywm [args] mywm.desktop, with that command
//
// The source is always explicit: an installed entry, the known-desktop table
// with --from-table, or a command after --. The table also fills in anything
// missing from the source; --no-table prevents it from doing so.
//
// It writes only to dm.LocalXSessions: never to standard output or another
// directory, especially /usr/share/xsessions, which belongs to packages.
// Without -i it only shows which file it would write and its contents.
func runEntry(args []string) error {
	fs := newFlagSet("entry", "[options] <name>\n"+
		"       uxsm entry [options] --exec <name> | --exec -- <command> [args...]\n"+
		"       uxsm entry [options] --plain --from-table <name> | --plain -- <command> [args...]",
		"Generate a session entry and install it in "+dm.LocalXSessions+".\n\n"+
			"  <name>          <name>-uxsm.desktop, which starts the session entry\n"+
			"                  <name>.desktop with `uxsm start <name>.desktop`\n"+
			"  --exec <name>   <name>-uxsm.desktop, which starts the command of\n"+
			"                  <name>.desktop with `uxsm start -D names -- command`\n"+
			"  --plain --from-table <name>\n"+
			"                  the plain <name>.desktop, without uxsm, from uxsm's\n"+
			"                  table of known desktops\n"+
			"  -- <command>    with --exec or --plain, an entry for that command,\n"+
			"                  named after its program\n\n"+
			"Every entry has an explicit source: an installed entry, uxsm's table of\n"+
			"known desktops selected with --from-table, or a command after --. Unless\n"+
			"--no-table is used, the table fills in metadata missing from that source:\n"+
			"desktop names, display name and comment.\n\n"+
			"Without -i, it only shows the file it would write.")
	exec := fs.Bool("exec", false, "make a -uxsm entry that starts the command directly")
	plain := fs.Bool("plain", false, "make the plain entry, without uxsm")
	install := fs.Bool("i", false, "write the entry (needs root)")
	force := fs.Bool("f", false, "overwrite an entry with the same name in "+dm.LocalXSessions+",\nor hide one in another xsessions directory, such as a package's")
	names := fs.String("D", "", "desktop `names` to add, separated by ':'")
	exclusive := fs.Bool("e", false, "use only the names given with -D, dropping the known ones")
	fromTable := fs.Bool("from-table", false, "use uxsm's table of known desktops as the source instead of\nan installed entry; requires --exec or --plain")
	noTable := fs.Bool("no-table", false, "do not use the table to fill metadata missing from the source;\n-D, -N and -C can provide it instead")
	name := fs.String("N", "", "the `name` shown on the login screen")
	comment := fs.String("C", "", "the `comment` shown on the login screen")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	dashes := afterDashes(args, fs.NArg())
	if *exec && *plain || fs.NArg() == 0 || !dashes && fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}
	if *fromTable && *noTable {
		return errors.New("--from-table selects the table as the source, while --no-table disables it; use only one")
	}
	if *fromTable && dashes {
		return errors.New("--from-table selects a command from uxsm's table and cannot be used with an explicit command after --")
	}
	if *fromTable && !*exec && !*plain {
		return errors.New("--from-table makes the entry from uxsm's table instead of from an installed one, so it needs --exec or --plain")
	}
	// A plain entry can only come from the table or a command, and the source
	// must be explicit, just as in every other case.
	if *plain && !dashes && !*fromTable {
		return errors.New("--plain does not wrap an installed entry; select the table with `--plain --from-table <name>`, or provide a command with `--plain -- <command>`")
	}
	if dashes && !*exec && !*plain {
		return errors.New("an entry that points to another entry needs that entry's name; for a command, use --exec or --plain")
	}

	src, err := entrySource(fs.Args(), dashes, *exec, *fromTable, !*noTable)
	if err != nil {
		return err
	}
	opts := sessionentry.Options{Names: *names, Exclusive: *exclusive, Name: *name, Comment: *comment}
	var e *sessionentry.Entry
	switch {
	case *plain:
		e, err = src.Plain(opts)
	case *exec:
		e, err = src.UxsmExec(opts)
	default:
		e, err = src.Uxsm(opts)
	}
	if err != nil {
		return err
	}

	dest := filepath.Join(dm.LocalXSessions, e.ID)
	if err := checkDestination(dest, e.ID, *force); err != nil {
		return err
	}
	warnDisplayManager()

	content := e.Render()
	if !*install {
		// Do not put a colon or period after a path: that way it can be copied
		// from the terminal with a double click without picking up punctuation.
		fmt.Printf("Would write this file, run it again with -i to write it (as root)\n  %s\n\n%s", dest, content)
		return nil
	}
	if err := os.MkdirAll(dm.LocalXSessions, 0o755); err != nil {
		return writeError(dest, err)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		return writeError(dest, err)
	}
	fmt.Printf("Wrote %s\n", dest)
	return nil
}

// entrySource decides where the entry comes from: a command, an existing entry,
// or the known-desktop table.
//
// fromTable is --from-table: the entry comes from the table, not an installed
// entry. table is the inverse of --no-table and says whether the table may fill
// in anything missing from the source.
func entrySource(args []string, dashes, exec, fromTable, table bool) (*sessionentry.Source, error) {
	if dashes {
		return sessionentry.FromCommand(args, table)
	}
	id := strings.TrimSuffix(args[0], ".desktop") + ".desktop"
	name := strings.TrimSuffix(id, ".desktop")

	if fromTable {
		return sessionentry.FromTable(name)
	}
	entry, err := desktopentry.Find(desktopentry.XSessions, id)
	if err == nil {
		return sessionentry.FromEntry(entry, table)
	}
	// Without an installed entry there is no source, and the table is not used
	// unless requested: explain how to request it if that desktop is known.
	if exec {
		if _, terr := sessionentry.FromTable(name); terr == nil {
			return nil, fmt.Errorf("cannot take the command from %s because that session entry is not installed; use uxsm's table with `uxsm entry --exec --from-table %s`, or provide the command with `uxsm entry --exec -- <command>`", id, name)
		}
		return nil, fmt.Errorf("cannot take the command from %s because that session entry is not installed, and %s is not in uxsm's table of known desktops; provide the command with `uxsm entry --exec -- <command>`", id, name)
	}
	// A uxsm entry that points to another entry needs that entry to exist. It is
	// not created automatically: explain how to proceed.
	if _, terr := sessionentry.FromTable(name); terr == nil {
		return nil, fmt.Errorf("there is no session entry %s to point to; create it first with `uxsm entry --plain --from-table %s`, or make one that starts the command directly with `uxsm entry --exec --from-table %s`", id, name, name)
	}
	return nil, fmt.Errorf("there is no session entry %s to point to, and %s is not in uxsm's table of known desktops; make one for its command with `uxsm entry --exec -- <command>`", id, name)
}

// checkDestination makes sure writing dest neither overwrites nor shadows
// another entry with the same name unless -f is used: either dest itself or an
// entry in another xsessions directory that dest would shadow because
// /usr/local/share takes precedence, such as one in /usr/share/xsessions from a
// package.
func checkDestination(dest, id string, force bool) error {
	if force {
		return nil
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists; use -f to overwrite it", dest)
	}
	for _, d := range xdg.DataDirs() {
		p := filepath.Join(d, desktopentry.XSessions, id)
		if filepath.Dir(p) == dm.LocalXSessions {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("there is already a session entry %s in %s, and %s would hide it: the display manager and uxsm start would use the new one; use -f to do it anyway", id, filepath.Dir(p), dest)
		}
	}
	return nil
}

// warnDisplayManager warns on standard error if the active display manager does
// not read dm.LocalXSessions: the entry would not appear on the login screen.
func warnDisplayManager() {
	r, err := dm.Active()
	if err != nil || r.Reads(dm.LocalXSessions) {
		return
	}
	fmt.Fprintf(os.Stderr, "uxsm: warning: %s, the display manager in use, does not read %s,\n"+
		"so this entry will not show on the login screen; to fix it: uxsm setup sessions-dir\n",
		r.Name, dm.LocalXSessions)
}

// writeError explains an error while writing the entry.
func writeError(dest string, err error) error {
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("writing %s: %w (run it as root)", dest, err)
	}
	return fmt.Errorf("writing %s: %w", dest, err)
}
