package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/heizeisaburou/uxsm/internal/dm"
)

// checkResult is the result of one uxsm check.
type checkResult struct {
	// status is "ok", "warning", or "unknown" when it cannot be determined.
	status string
	// summary states the result; details explain it.
	summary string
	details []string
}

// checks are the uxsm checks in display order. Each carries the name that
// starts its output line.
var checks = []struct {
	name string
	run  func() checkResult
}{
	{"sessions dirs", checkSessionsDirs},
}

// errWarnings is returned when any uxsm check produces a warning. It maps to
// exit status 1 so scripts can use the result.
var errWarnings = errors.New("some checks gave warnings")

// runCheck runs every check for `uxsm check`. Without a subcommand it runs them
// all, and each line states what was checked and the result.
//
// The only subcommand is `is-active`, which asks about the current session
// rather than checking system configuration. It lives here because this is
// where uwsm users expect to find the equivalent command.
func runCheck(args []string) error {
	if len(args) > 0 && args[0] == "is-active" {
		return runIsActive(args[1:])
	}

	fs := newFlagSet("check", "",
		"Run every uxsm check and show what each one checked and its result:\n"+
			"ok, warning, or unknown when it cannot be determined. Exits with 1 if\n"+
			"any check gives a warning.\n\n"+
			"Checks:\n"+
			"  sessions dirs  whether the display manager in use reads the local session\n"+
			"                 directories, where uxsm installs the entries it generates\n\n"+
			"Subcommand:\n"+
			"  is-active      exit with 0 if a uxsm session is running, 1 if not")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return errUsage
	}

	warnings := false
	for _, c := range checks {
		r := c.run()
		fmt.Printf("%s: %s: %s\n", c.name, r.status, r.summary)
		for _, d := range r.details {
			fmt.Printf("  %s\n", d)
		}
		warnings = warnings || r.status == "warning"
	}
	if warnings {
		return errWarnings
	}
	return nil
}

// checkSessionsDirs checks whether the active display manager reads
// dm.LocalXSessions. If it does not, entries installed by uxsm entry do not
// appear on the login screen.
func checkSessionsDirs() checkResult {
	r, err := dm.Active()
	if errors.Is(err, dm.ErrNoDisplayManager) {
		return checkResult{status: "unknown", summary: err.Error(),
			details: []string{"without a display manager there is no login screen that lists session entries"}}
	}
	if err != nil {
		return checkResult{status: "unknown", summary: err.Error()}
	}

	details := []string{
		"it reads: " + strings.Join(r.Dirs, ", "),
		"from: " + r.Origin,
	}
	missing := r.Missing()
	if len(missing) == 0 {
		return checkResult{status: "ok",
			summary: r.Name + " (in use) reads " + strings.Join(dm.LocalSessions, " and "), details: details}
	}
	details = append(details,
		"session entries installed there will not show on the login screen",
		"to fix it: uxsm setup sessions-dir")
	return checkResult{status: "warning",
		summary: r.Name + " (in use) does not read " + strings.Join(missing, " or "), details: details}
}
