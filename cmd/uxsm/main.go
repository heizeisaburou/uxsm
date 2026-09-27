// Command uxsm starts and manages graphical X11 sessions with systemd --user,
// as uwsm does for Wayland.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// version is set by the Makefile at build time: go build -ldflags "-X main.version=…".
var version = "dev"

// command is a subcommand: its name, a one-line summary, the function that runs
// it with the arguments after its name, and whether it is hidden from general
// help.
type command struct {
	name    string
	summary string
	run     func(args []string) error
	hidden  bool
}

// group is a set of subcommands with its own help. uxsm has two: the top-level
// group (start, stop…) and the aux group (exec, waitpid). Both use the same
// dispatch code, so they behave consistently.
type group struct {
	// name is how the group is written on the command line: "uxsm" or "uxsm aux".
	name        string
	description string
	commands    []command
}

// rootCommands contains uxsm's subcommands in help order. aux is hidden because
// systemd units, rather than people, call it, but it runs like any other command.
var rootCommands = group{
	name:        "uxsm",
	description: "Start and manage X11 sessions under systemd --user.",
	commands: []command{
		{"start", "start an X11 session from a session entry or a command", runStart, false},
		{"stop", "stop the running session", runStop, false},
		{"finalize", "tell uxsm from the desktop that the session is ready", runFinalize, false},
		{"app", "run an application in its own unit, inside the session", runApp, false},
		{"entry", "generate a session entry and install it", runEntry, false},
		{"check", "check that the system is ready for uxsm, or if a session is running", runCheck, false},
		{"setup", "change the system so that uxsm works fully", runSetup, false},
		{"version", "print the version", runVersion, false},
		{"aux", "internal commands used by uxsm's systemd units", runAux, true},
	},
}

func main() {
	os.Exit(exitCode(rootCommands.dispatch(os.Args[1:])))
}

// dispatch selects the subcommand from the first argument and passes through
// the rest unchanged.
//
// `help`, `-h`, and `--help` are interpreted here only in the first position;
// any later help request belongs to the subcommand.
func (g group) dispatch(args []string) error {
	if len(args) == 0 {
		g.usage(os.Stderr)
		return errUsage
	}

	name, rest := args[0], args[1:]
	if name == "help" || isHelpFlag(name) {
		g.usage(os.Stdout)
		return nil
	}
	for _, c := range g.commands {
		if c.name == name {
			return c.run(rest)
		}
	}

	fmt.Fprintf(os.Stderr, "%s: unknown command %q\n\n", g.name, name)
	g.usage(os.Stderr)
	return errUsage
}

// usage writes the group's help without hidden subcommands.
func (g group) usage(w io.Writer) {
	fmt.Fprintf(w, "Usage: %s <command> [options]\n\n%s\n\nCommands:\n", g.name, g.description)
	for _, c := range g.commands {
		if !c.hidden {
			fmt.Fprintf(w, "  %-12s %s\n", c.name, c.summary)
		}
	}
	fmt.Fprintf(w, "\nRun \"%s <command> -h\" for help on a command.\n", g.name)
}

// errUsage marks argument errors that have already displayed their help.
var errUsage = errors.New("usage")

// errNo is the "no" answer from a command that asks a question, such as
// is-active: it exits with status 1 like an error, but writes nothing.
var errNo = errors.New("no")

// exitCode translates a subcommand result into an exit status: 0 on success or
// requested help, 2 for invalid arguments, and 1 for any other error, which is
// also written out.
func exitCode(err error) int {
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errNo):
		return 1
	case errors.Is(err, errUsage):
		return 2
	default:
		fmt.Fprintf(os.Stderr, "uxsm: %v\n", err)
		return 1
	}
}

// wantsHelp says whether args requests help anywhere before "--".
//
// flag stops parsing options at the first non-option argument, so by itself it
// would miss the -h in `uxsm start bspwm.desktop -h`. Scanning the whole line
// lets users append -h to a partially written command and see its help. Anything
// after "--" belongs to the launched program, not uxsm.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if isHelpFlag(a) {
			return true
		}
	}
	return false
}

// isHelpFlag says whether a is a help request. These are the four forms
// recognized by package flag, so the same forms work anywhere: `uxsm -help`
// and `uxsm start bspwm.desktop -help`.
func isHelpFlag(a string) bool {
	switch a {
	case "-h", "-help", "--h", "--help":
		return true
	}
	return false
}

// parseFlags parses a subcommand's options. If help is requested anywhere, it
// writes help to standard output and returns flag.ErrHelp, which produces exit
// status 0. If an option is invalid, flag has already written the error and help
// to standard error, and parseFlags returns errUsage, which produces status 2.
func parseFlags(fs *flag.FlagSet, args []string) error {
	if wantsHelp(args) {
		fs.SetOutput(os.Stdout)
		fs.Usage()
		return flag.ErrHelp
	}
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	return nil
}

// newFlagSet creates a subcommand's option set with its own help: the usage
// line, a description, and any options it defines.
func newFlagSet(name, usageLine, description string) *flag.FlagSet {
	fs := flag.NewFlagSet("uxsm "+name, flag.ContinueOnError)
	fs.Usage = func() {
		w := fs.Output()
		// With no usageLine, do not leave a stray space after the name.
		fmt.Fprintf(w, "Usage: %s\n\n%s\n", strings.TrimSpace("uxsm "+name+" "+usageLine), description)
		hasFlags := false
		fs.VisitAll(func(*flag.Flag) { hasFlags = true })
		if hasFlags {
			fmt.Fprint(w, "\nOptions:\n")
			fs.PrintDefaults()
		}
	}
	return fs
}

func runVersion(args []string) error {
	fs := newFlagSet("version", "", "Print the uxsm version.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	fmt.Println(version)
	return nil
}
