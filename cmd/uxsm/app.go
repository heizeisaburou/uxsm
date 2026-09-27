package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/heizeisaburou/uxsm/internal/appunit"
	"github.com/heizeisaburou/uxsm/internal/desktopentry"
)

// runApp launches an application in its own systemd unit:
//
//	uxsm app -- kitty
//	uxsm app firefox.desktop
//	uxsm app firefox.desktop:new-private-window
//	uxsm app -s b -- fcitx5
//
// This is what `uwsm app` does on Wayland. Without it, everything started by a
// desktop hangs off the desktop and appears together; with it, each application
// has its own unit in one of the session slices: it appears separately in
// systemctl, can be limited, logs to the journal under its own name, and stops
// with the session.
func runApp(args []string) error {
	fs := newFlagSet("app", "[-s slice] [-t scope|service] [-p Key=Value] [options] [--] <command> [args...]\n"+
		"       uxsm app [options] <entry.desktop>[:action] [files or URLs...]",
		"Run an application in its own systemd unit, inside the graphical slices of\n"+
			"the session: it shows up on its own and stops with the session.\n"+
			"An argument ending in .desktop is a desktop entry, optionally with an\n"+
			"action after ':'; anything else, or anything after --, is a command.")
	slice := fs.String("s", "", "`slice` of the session: a (applications, the default),\n"+
		"b (background), s (session), or a name ending in .slice")
	unitType := fs.String("t", "scope", "unit `type`: scope, the default, or service")
	appName := fs.String("a", "", "`name` of the application within the unit name")
	unitName := fs.String("u", "", "the whole `unit` name, instead of the generated one")
	description := fs.String("d", "", "unit `description`")
	silent := fs.String("S", "", "throw away the application's `output`: out, err or both;\n"+
		"only for a service, a scope inherits the output of its caller")
	var properties stringList
	fs.Var(&properties, "p", "systemd unit `property` as Key=Value, in the form accepted by\n"+
		"systemd-run: TimeoutStopSec=5, MemoryMax=2G… Can be repeated")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return errUsage
	}

	o := appunit.Options{
		Properties:  properties,
		AppName:     *appName,
		UnitName:    *unitName,
		Description: *description,
		Silent:      *silent,
	}
	switch *unitType {
	case "scope":
	case "service":
		o.Service = true
	default:
		fmt.Fprintf(os.Stderr, "uxsm app: -t is %q, but a unit is either a scope or a service\n", *unitType)
		return errUsage
	}
	if o.Silent != "" && !o.Service {
		fmt.Fprintln(os.Stderr, "uxsm app: -S only works with -t service: a scope inherits the output of its caller")
		return errUsage
	}

	var err error
	if o.Slice, err = appunit.Slice(*slice); err != nil {
		return err
	}
	if err := resolveApp(&o, fs.Args(), afterDashes(args, fs.NArg())); err != nil {
		return err
	}

	runArgs, err := o.RunArgs()
	if err != nil {
		return err
	}
	path, err := exec.LookPath(runArgs[0])
	if err != nil {
		return err
	}
	// With exec, the scope keeps this process: the application is a child of
	// whoever requested its launch, which is what a scope means.
	return syscall.Exec(path, runArgs, os.Environ())
}

// resolveApp decides what to launch: an application entry, including the
// requested action, or a command unchanged. dashes says whether the arguments
// followed "--", in which case they are always a command.
func resolveApp(o *appunit.Options, args []string, dashes bool) error {
	id, action, _ := strings.Cut(args[0], ":")
	if dashes || !strings.HasSuffix(id, ".desktop") {
		if _, err := exec.LookPath(args[0]); err != nil {
			return err
		}
		o.Argv = args
		return nil
	}

	entry, err := desktopentry.Find(desktopentry.Applications, id)
	if err != nil {
		return err
	}
	if entry.Terminal {
		return fmt.Errorf("%s asks to run inside a terminal, which uxsm app does not do yet: "+
			"run it with your terminal, as `uxsm app -- <terminal> -e %s`", entry.ID, entry.Exec)
	}

	command := entry.Exec
	if action != "" {
		a, ok := entry.Actions[action]
		if !ok {
			return fmt.Errorf("%s has no action %q", entry.ID, action)
		}
		if a.Exec == "" {
			return fmt.Errorf("the action %q of %s has no Exec=", action, entry.ID)
		}
		command = a.Exec
	}

	argv, err := desktopentry.SplitExecFields(command, desktopentry.Fields{
		Args: args[1:],
		Icon: entry.Icon,
		Name: entry.Name,
		Path: entry.Path,
	})
	if err != nil {
		return fmt.Errorf("%s: %w", entry.Path, err)
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return err
	}

	o.Argv = argv
	o.WorkingDir = entry.WorkingDir
	if o.Entry == "" {
		o.Entry = strings.TrimSuffix(entry.ID, ".desktop")
	}
	if o.Description == "" {
		o.Description = first(entry.Name, entry.Comment)
	}
	return nil
}

// stringList is a repeatable option, like systemd-run's -p: every occurrence
// appends its value to the list.
type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, " ") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

// first returns the first non-empty value.
func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
