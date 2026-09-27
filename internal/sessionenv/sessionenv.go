package sessionenv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/heizeisaburou/uxsm/internal/session"
	"github.com/heizeisaburou/uxsm/internal/systemd"
)

// Working files in session.RuntimeDir in addition to those left by uxsm start.
const (
	// preFile is the manager snapshot from before session preparation.
	preFile = "env_pre"
	// cleanupFile contains names to remove at shutdown.
	cleanupFile = "env_cleanup"
)

// Prepare builds the session environment in the manager. It is
// `uxsm aux prepare-env`, the ExecStart= of uxsm-env@.service, and runs before
// the desktop.
//
//  1. Save a snapshot of the manager environment.
//  2. Run the loader with the snapshot, overlaid by the login environment and
//     session identity, and obtain the resulting environment.
//  3. Compute what to import and remove (computeChanges), record what must be
//     removed at shutdown, and apply it to the manager and, if needed, D-Bus.
//
// Cleanup is recorded before modifying the manager so it knows what to undo if
// an operation fails halfway through.
func Prepare() error {
	dir, err := session.RuntimeDir()
	if err != nil {
		return err
	}
	login, err := session.ReadEnvFile(filepath.Join(dir, session.LoginFile))
	if err != nil {
		return fmt.Errorf("reading the login environment saved by uxsm start: %w", err)
	}
	identity, err := session.ReadEnvFile(filepath.Join(dir, session.IdentityFile))
	if err != nil {
		return fmt.Errorf("reading the session identity saved by uxsm start: %w", err)
	}

	// Filter the snapshot like the resulting environment; otherwise values the
	// filter removes from the result (SHELL…) would appear missing and be removed
	// from the manager.
	pre, err := systemd.Environment()
	if err != nil {
		return err
	}
	pre = session.FilterEnv(pre)
	if err := session.WriteEnvFile(filepath.Join(dir, preFile), pre); err != nil {
		return fmt.Errorf("saving the systemd environment snapshot: %w", err)
	}

	// The login environment overrides the manager environment, as in uwsm.
	base := assignments(mergeEnv(envMap(pre), envMap(login)))
	post, err := runLoader(base, identity)
	if err != nil {
		return err
	}

	c := computeChanges(pre, post)
	if err := session.WriteEnvFile(filepath.Join(dir, cleanupFile), c.cleanup); err != nil {
		return fmt.Errorf("saving the cleanup list: %w", err)
	}

	fmt.Printf("Exporting to the systemd user manager: %v\n", names(c.set))
	if err := systemd.SetEnvironment(c.set...); err != nil {
		return err
	}
	if len(c.unset) > 0 {
		fmt.Printf("Removing from the systemd user manager: %v\n", c.unset)
		if err := systemd.UnsetEnvironment(c.unset...); err != nil {
			return err
		}
	}
	if !systemd.DBusIsBroker() {
		if err := systemd.UpdateDBusActivationEnvironment(c.set); err != nil {
			fmt.Fprintf(os.Stderr, "uxsm: updating the D-Bus activation environment: %v\n", err)
		}
	}
	return nil
}

// Cleanup restores the manager environment to its state before the session. It
// is `uxsm aux cleanup-env`, the ExecStopPost= of uxsm-env@.service, so it runs
// whenever the service stops, including after failed preparation.
//
//  1. Remove names selected by cleanupNames.
//  2. Restore the snapshot.
//  3. Remove the session's working files.
//
// Without a snapshot there is nothing to undo: preparation never began.
func Cleanup() error {
	dir, err := session.RuntimeDir()
	if err != nil {
		return err
	}
	pre, err := session.ReadEnvFile(filepath.Join(dir, preFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	marked, err := session.ReadEnvFile(filepath.Join(dir, cleanupFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	now, err := systemd.Environment()
	if err != nil {
		return err
	}

	toUnset := cleanupNames(pre, now, marked)
	if len(toUnset) > 0 {
		fmt.Printf("Removing from the systemd user manager: %v\n", toUnset)
		if !systemd.DBusIsBroker() {
			empty := make([]string, len(toUnset))
			for i, n := range toUnset {
				empty[i] = n + "="
			}
			if err := systemd.UpdateDBusActivationEnvironment(empty); err != nil {
				fmt.Fprintf(os.Stderr, "uxsm: clearing the D-Bus activation environment: %v\n", err)
			}
		}
		if err := systemd.UnsetEnvironment(toUnset...); err != nil {
			return err
		}
	}

	fmt.Println("Restoring the systemd user manager environment from before the session.")
	if err := systemd.SetEnvironment(pre...); err != nil {
		return err
	}

	for _, f := range []string{preFile, cleanupFile, session.LoginFile, session.IdentityFile,
		session.CommandFile, session.AutostartFile, session.ReadyFile} {
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// mergeEnv returns base with variables from over taking precedence.
func mergeEnv(base, over map[string]string) map[string]string {
	m := make(map[string]string, len(base)+len(over))
	for k, v := range base {
		m[k] = v
	}
	for k, v := range over {
		m[k] = v
	}
	return m
}

// names extracts assignment names for messages.
func names(env []string) []string {
	out := make([]string, 0, len(env))
	for name := range envMap(env) {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
