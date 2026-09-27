package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/heizeisaburou/uxsm/internal/autostart"
	"github.com/heizeisaburou/uxsm/internal/desktopentry"
	"github.com/heizeisaburou/uxsm/internal/pidwait"
	"github.com/heizeisaburou/uxsm/internal/session"
	"github.com/heizeisaburou/uxsm/internal/sessionenv"
	"github.com/heizeisaburou/uxsm/internal/systemd"
	"github.com/heizeisaburou/uxsm/internal/x11"
)

// auxCommands are the internal subcommands called from Exec*= by uxsm's
// systemd units. They appear in `uxsm aux -h`, not in the top-level help.
var auxCommands = group{
	name:        "uxsm aux",
	description: "Internal commands used by uxsm's systemd units.",
	commands: []command{
		{"exec", "replace this process with the desktop of the session", runAuxExec, false},
		{"waitpid", "wait until a process exits", runAuxWaitPID, false},
		{"wait-ready", "wait until the session is ready", runAuxWaitReady, false},
		{"autostart", "start the XDG autostart entries of the session", runAuxAutostart, false},
		{"prepare-env", "set up the session environment in the systemd user manager", runAuxPrepareEnv, false},
		{"cleanup-env", "restore the systemd user manager environment from before the session", runAuxCleanupEnv, false},
	},
}

// runAux dispatches `uxsm aux <command>` like the top-level command dispatcher.
func runAux(args []string) error {
	return auxCommands.dispatch(args)
}

// runAuxExec executes the session desktop: `uxsm aux exec bspwm.desktop`, or
// `uxsm aux exec bspwm` when the session was started from a command.
//
// It is the ExecStart= of uxsm-desktop@.service, with the instance as its
// argument. It does not receive the command on its command line: for an entry,
// it reads the entry again; for a command, it reads what uxsm start saved. This
// lets the unit contain only the instance (%i), and lets systemd display a
// readable name in its status instead of a long command.
//
// It replaces itself with the program through exec so the desktop itself is the
// service's main process: when the desktop exits, the service exits.
func runAuxExec(args []string) error {
	fs := newFlagSet("aux exec", "<entry.desktop | command-name>",
		"Replace this process with the desktop of the session: the Exec= of a\n"+
			"session entry, or the command saved by uxsm start.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}

	argv, err := desktopCommand(fs.Arg(0))
	if err != nil {
		return err
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	return syscall.Exec(path, argv, os.Environ())
}

// desktopCommand returns the desktop command for instance id: the entry's Exec=
// when id ends in .desktop, or the command saved by uxsm start.
func desktopCommand(id string) ([]string, error) {
	if strings.HasSuffix(id, ".desktop") {
		entry, err := desktopentry.Find(desktopentry.XSessions, id)
		if err != nil {
			return nil, err
		}
		argv, err := desktopentry.SplitExec(entry.Exec)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Path, err)
		}
		return argv, nil
	}

	dir, err := session.RuntimeDir()
	if err != nil {
		return nil, err
	}
	argv, err := session.ReadEnvFile(filepath.Join(dir, session.CommandFile))
	if err != nil {
		return nil, fmt.Errorf("reading the command saved by uxsm start: %w", err)
	}
	// Only one session exists at a time, but a file for another instance is
	// leftover state from another session and must not be executed.
	if len(argv) == 0 || filepath.Base(argv[0]) != id {
		return nil, fmt.Errorf("the command saved by uxsm start, %q, is not the one of %s", argv, id)
	}
	return argv, nil
}

// runAuxWaitPID waits for a process to exit: `uxsm aux waitpid 1234`.
//
// It is the ExecStart= of uxsm-bindpid@.service, with the session process PID as
// its instance. When it returns, the service exits and its OnSuccess= starts
// uxsm-shutdown.target, which shuts down the session.
func runAuxWaitPID(args []string) error {
	fs := newFlagSet("aux waitpid", "<pid>", "Wait until a process exits; any process, not only a child.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}
	pid, err := strconv.Atoi(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("invalid PID %q", fs.Arg(0))
	}
	return pidwait.Wait(pid)
}

// readyInterval is how often readiness is checked. The check is cheap—two
// properties over a Unix socket and one file—and half a second of stalled
// desktop is noticeable, so it runs frequently.
const readyInterval = 100 * time.Millisecond

// runAuxWaitReady waits for session readiness: `uxsm aux wait-ready`.
//
// It is the ExecStartPost= of uxsm-desktop@.service. systemd does not consider
// the service started until ExecStartPost= finishes, and uxsm-session@.target
// and graphical-session.target are ordered after the service. Anything started
// with the session therefore finds a desktop already on screen.
//
// The wait has no limit of its own: TimeoutStartSec= in the unit provides it.
// When it expires, systemd kills this process, the service fails, and its
// OnFailure= shuts down the session, returning control to the display manager.
func runAuxWaitReady(args []string) error {
	fs := newFlagSet("aux wait-ready", "",
		"Wait until the session is ready: either an EWMH window manager takes\n"+
			"over the X display, or the desktop runs uxsm finalize.")
	timeout := fs.Duration("timeout", 0, "give up after this `duration`; zero waits with no limit of its own")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return errUsage
	}

	reason, err := waitReady(*timeout)
	if err != nil {
		return err
	}
	fmt.Println(reason)
	return nil
}

// waitReady waits for the session-ready signal and returns what triggered it.
//
// Two paths race, and the first wins: an EWMH window manager observed by uxsm
// through the X server, and `uxsm finalize` run by the desktop. Signaling is one
// atomic filesystem operation (session.SignalReady), so only one counts even if
// both arrive together.
func waitReady(timeout time.Duration) (string, error) {
	// The desktop may have called uxsm finalize before this wait even starts; in
	// that case there is nothing to wait for and no reason to connect to X.
	if on, reason, err := session.Ready(); err != nil || on {
		return reason, err
	}

	c, err := x11.Dial("")
	if err != nil {
		return "", err
	}
	defer c.Close()

	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		wm, err := c.Manager()
		if err != nil {
			return "", err
		}
		if wm != nil {
			if _, err := session.SignalReady("window manager ready: " + wm.String()); err != nil {
				return "", err
			}
			// If uxsm finalize won by a small margin, its reason is authoritative
			// because that is the one stored in the signal file.
			_, reason, err := session.Ready()
			return reason, err
		}
		if on, reason, err := session.Ready(); err != nil || on {
			return reason, err
		}
		if !deadline.IsZero() && !time.Now().Add(readyInterval).Before(deadline) {
			return "", fmt.Errorf("the session was not ready after %s: no EWMH window manager took over "+
				"the X display, and the desktop did not run uxsm finalize", timeout)
		}
		time.Sleep(readyInterval)
	}
}

// runAuxAutostart starts the session's XDG autostart when uxsm owns it:
// `uxsm aux autostart bspwm.desktop`.
//
// It is the second ExecStartPost= of uxsm-desktop@.service, after the window
// manager wait, so autostart applications start with the desktop already visible.
//
// It starts the target only if uxsm start left the marker saying uxsm owns
// autostart. If absent, it does nothing, as required for a desktop that starts
// its own entries. The decision cannot use a unit Condition=: dependencies are
// resolved when the job is assembled, before conditions are checked, so
// autostart would still be pulled in even when the unit was skipped.
//
// One of our units must pull it in rather than invoking `systemctl start
// xdg-desktop-autostart.target`: the standard target has RefuseManualStart= and
// can only be started as another unit's dependency.
func runAuxAutostart(args []string) error {
	fs := newFlagSet("aux autostart", "<entry.desktop | command-name>",
		"Start the XDG autostart entries of the session, if uxsm start decided\n"+
			"that starting them is up to uxsm.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage
	}

	dir, err := session.RuntimeDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, session.AutostartFile)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	// The drop-in moves entries to app-uxsm.slice, and the manager sees it only
	// if it reloads before loading those units.
	if err := autostart.Write(); err != nil {
		return fmt.Errorf("writing the autostart drop-in: %w", err)
	}
	if err := systemd.DaemonReload(); err != nil {
		return err
	}
	fmt.Println("Starting the XDG autostart entries of the session.")
	return systemd.StartNoBlock(systemd.AutostartTarget(fs.Arg(0)))
}

// runAuxPrepareEnv installs the session environment in the user manager:
// `uxsm aux prepare-env`.
//
// It is the ExecStart= of uxsm-env@.service, which runs before the desktop. It
// reads what uxsm start left in $XDG_RUNTIME_DIR/uxsm; the implementation is in
// sessionenv.Prepare.
func runAuxPrepareEnv(args []string) error {
	fs := newFlagSet("aux prepare-env", "", "Set up the session environment in the systemd user manager.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return errUsage
	}
	return sessionenv.Prepare()
}

// runAuxCleanupEnv restores the user-manager environment:
// `uxsm aux cleanup-env`.
//
// It is the ExecStopPost= of uxsm-env@.service, so it runs whenever the service
// stops, regardless of what initiated shutdown. The implementation is in
// sessionenv.Cleanup.
func runAuxCleanupEnv(args []string) error {
	fs := newFlagSet("aux cleanup-env", "", "Restore the systemd user manager environment from before the session.")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return errUsage
	}
	err := sessionenv.Cleanup()
	// Always remove the autostart drop-in, even if this session never wrote it
	// and even if environment cleanup failed.
	if rerr := autostart.Remove(); rerr != nil && err == nil {
		err = fmt.Errorf("removing the autostart drop-in: %w", rerr)
	}
	if err == nil {
		err = systemd.DaemonReload()
	}
	return err
}
