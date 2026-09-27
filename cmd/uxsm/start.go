package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/heizeisaburou/uxsm/internal/desktopentry"
	"github.com/heizeisaburou/uxsm/internal/session"
	"github.com/heizeisaburou/uxsm/internal/systemd"
)

// runStart starts a session from an entry or command, like uwsm:
//
//	uxsm start [-e] [-D names] bspwm.desktop
//	uxsm start [-e] [-D names] [--] bspwm [arguments...]
//
// The display manager executes this function, so this is the process it watches:
//  1. Decide what to start (resolveTarget): an entry found in the xsessions
//     directories, or a command. Also verify that a D-Bus session bus exists.
//     Missing or malformed input therefore fails before systemd is touched.
//  2. Compute the session identity—the desktop names and corresponding XDG_*
//     variables—from -D and -e.
//  3. Wait until no previous session remains in the middle of shutdown: its
//     cleanup would otherwise remove this session's files.
//  4. Save the display manager's environment and the identity under
//     $XDG_RUNTIME_DIR/uxsm, plus the command for direct-command sessions. This
//     lets uxsm-env@.service install the session environment in the user manager
//     before starting the desktop and clean it at shutdown. Also leave the marker
//     that records whether uxsm starts XDG autostart for this session
//     (markAutostart).
//  5. Start uxsm-bindpid@<pid>.service with this process's PID so the session
//     shuts down if the display manager kills it.
//  6. Replace this process with `systemctl --user start --wait
//     uxsm-desktop@<id>.service`, which returns only after the desktop exits.
//     exec preserves the PID, so bindpid continues watching the session process.
func runStart(args []string) error {
	fs := newFlagSet("start", "[-e] [-D names] [--no-autostart] <entry.desktop>\n"+
		"       uxsm start [-e] [-D names] [--no-autostart] [--] <command> [args...]",
		"Start an X11 session, running its desktop as a systemd user service: the\n"+
			"Exec= of a session entry from the xsessions directories, or a command.\n"+
			"An argument ending in .desktop is an entry; anything else, or anything\n"+
			"after --, is a command.")
	namesFlag := fs.String("D", "", "desktop `names` for XDG_CURRENT_DESKTOP, separated by ':'")
	exclusive := fs.Bool("e", false, "use only the names given with -D, discarding the existing\n"+
		"XDG_CURRENT_DESKTOP and the entry's DesktopNames=")
	noAutostart := fs.Bool("no-autostart", false, "do not start the XDG autostart entries of the session;\n"+
		"uxsm starts them by default, as uwsm does")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return errUsage
	}
	target, err := resolveTarget(fs.Args(), afterDashes(args, fs.NArg()))
	if err != nil {
		return err
	}
	if err := systemd.CheckInstance(target.id); err != nil {
		return err
	}
	if err := systemd.CheckUserBus(); err != nil {
		return err
	}

	names, err := session.DesktopNames(session.NamesOptions{
		Current:    os.Getenv("XDG_CURRENT_DESKTOP"),
		Entry:      target.names,
		Flag:       *namesFlag,
		Exclusive:  *exclusive,
		Executable: filepath.Base(target.argv[0]),
	})
	if errors.Is(err, session.ErrBadNames) {
		fmt.Fprintf(os.Stderr, "uxsm start: %v\n", err)
		return errUsage
	}
	if err != nil {
		return err
	}
	identity := session.IdentityVars(names)

	if err := waitForPreviousSession(previousSessionTimeout); err != nil {
		return err
	}

	dir, err := session.RuntimeDir()
	if err != nil {
		return err
	}
	if err := session.WriteEnvFile(filepath.Join(dir, session.LoginFile), session.FilterEnv(os.Environ())); err != nil {
		return fmt.Errorf("saving the login environment: %w", err)
	}
	if err := session.WriteEnvFile(filepath.Join(dir, session.IdentityFile), identity); err != nil {
		return fmt.Errorf("saving the session identity: %w", err)
	}
	if target.command {
		if err := session.WriteEnvFile(filepath.Join(dir, session.CommandFile), target.argv); err != nil {
			return fmt.Errorf("saving the command: %w", err)
		}
	}
	// Clear a signal possibly left by a previous session that never cleaned up;
	// otherwise this session would become ready with no desktop on screen.
	if err := session.ClearReady(); err != nil {
		return fmt.Errorf("clearing the ready signal of a previous session: %w", err)
	}
	if err := markAutostart(dir, *noAutostart); err != nil {
		return fmt.Errorf("deciding on the XDG autostart: %w", err)
	}

	if err := systemd.Start(systemd.BindPIDUnit(os.Getpid())); err != nil {
		return fmt.Errorf("binding the session to its process: %w", err)
	}
	return systemd.ExecStartWait(systemd.DesktopUnit(target.id))
}

// markAutostart records in the runtime directory whether uxsm starts XDG
// autostart for this session. The desktop's ExecStartPost=, `uxsm aux
// autostart`, reads it.
//
// It is enabled unless explicitly disabled, like uwsm: a systemd-managed
// session is expected to start autostart entries itself. uxsm start does not
// identify desktops here; uxsm entry knows that and writes --no-autostart for
// desktops that start their own entries.
func markAutostart(dir string, disabled bool) error {
	path := filepath.Join(dir, session.AutostartFile)
	if disabled {
		fmt.Println("Not starting the XDG autostart entries of the session: --no-autostart.")
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	return os.WriteFile(path, []byte("uxsm start, without --no-autostart\n"), 0o600)
}

// startTarget describes what uxsm start will launch.
type startTarget struct {
	// id is the session-unit instance: the entry ID, "bspwm.desktop", or the
	// command's program name, "bspwm".
	id string
	// argv is what the desktop service will execute.
	argv []string
	// names are the entry's DesktopNames=; a command has none.
	names []string
	// command says this is a direct command. uxsm start then saves it for uxsm
	// aux exec; for an entry, aux exec reads the entry again instead.
	command bool
}

// resolveTarget decides what uxsm start launches from the arguments remaining
// after its options. dashes says whether they followed "--".
//
// A single argument ending in .desktop without "--" is an entry; everything
// else is a command and its arguments. This matches uwsm. "--" also has the
// same purpose: passing program arguments beginning with "-" that uxsm would
// otherwise parse as its own options.
func resolveTarget(args []string, dashes bool) (*startTarget, error) {
	if !dashes && len(args) == 1 && strings.HasSuffix(args[0], ".desktop") {
		entry, err := desktopentry.Find(desktopentry.XSessions, args[0])
		if err != nil {
			return nil, err
		}
		argv, err := desktopentry.SplitExec(entry.Exec)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Path, err)
		}
		return &startTarget{id: entry.ID, argv: argv, names: entry.DesktopNames}, nil
	}

	// As with a missing entry, fail now rather than when the desktop service
	// attempts to execute it.
	if _, err := exec.LookPath(args[0]); err != nil {
		return nil, err
	}
	return &startTarget{id: filepath.Base(args[0]), argv: args, command: true}, nil
}

// afterDashes reports whether the last n arguments in args followed "--". It
// is needed because package flag consumes "--" without reporting it.
func afterDashes(args []string, n int) bool {
	i := len(args) - n
	return i > 0 && args[i-1] == "--"
}

// previousSessionTimeout is how long uxsm start waits for a previous session to
// finish shutting down. Shutdown should take little longer than cleanup; if a
// session remains after this interval, another session is running.
const previousSessionTimeout = 10 * time.Second

// sessionUnits are the units of this user's graphical session: those of uxsm
// and uwsm, the other manager using this model. A live unit means a session is
// either running or still shutting down.
//
// Two graphical sessions for one user are incompatible regardless of their
// manager: there is one systemd manager per user, so both would share
// graphical-session.target and the environment. Closing one would stop the
// other's target, and one session's cleanup would erase the other's new values.
//
// This uses concrete unit names instead of graphical-session.target, which is
// owned by systemd and may be activated by anyone. The NixOS session wrapper
// activates it before running the entry's Exec=; checking it made uxsm mistake
// itself for a previous session and refuse to start.
var sessionUnits = append(uxsmUnits,
	// uwsm's units, which manage Wayland sessions using the same model.
	"wayland-session-shutdown.target", "wayland-wm@*.service", "wayland-wm-env@*.service",
	"wayland-session@*.target", "wayland-session-pre@*.target", "wayland-session-bindpid@*.service",
)

// uxsmUnits belong to an uxsm session and are reported by `uxsm check is-active`.
var uxsmUnits = []string{
	"uxsm-shutdown.target",
	"uxsm-desktop@*.service", "uxsm-env@*.service", "uxsm-session@*.target", "uxsm-bindpid@*.service",
}

// waitForPreviousSession waits up to timeout until no unit from another
// graphical session remains alive.
//
// Session shutdown is not instantaneous: while stopping, its environment
// service cleans the manager and removes files from the $XDG_RUNTIME_DIR/uxsm
// directory. If a new session started at that moment—for example through fast
// autologin—the old
// cleanup would remove the new files.
func waitForPreviousSession(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		live, err := systemd.LiveUnits(sessionUnits...)
		if err != nil {
			return err
		}
		if len(live) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("another graphical session is still running: %s", strings.Join(live, ", "))
		}
		time.Sleep(100 * time.Millisecond)
	}
}
