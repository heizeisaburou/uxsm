package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadyFile signals that the session is ready: a desktop is on screen and
// anything started afterwards has somewhere to appear.
//
// Two equivalent paths can turn it on: uxsm sees an EWMH window manager, or the
// desktop runs `uxsm finalize`, as with uwsm. Downstream units—the session
// target, graphical-session.target, and autostart—do not distinguish the path.
const ReadyFile = "ready"

// SignalReady turns on the session-ready signal. reason identifies who turned
// it on and is recorded for the journal.
//
// It reports whether this call turned the signal on. false is not an error: the
// other path arrived first, and the signal is enabled only once. The system,
// not uxsm, resolves the race: the file is created with O_EXCL, so only one of
// two simultaneous paths can win.
func SignalReady(reason string) (bool, error) {
	path, err := readyPath()
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, reason); err != nil {
		return true, err
	}
	return true, nil
}

// Ready says whether the signal is on and why it was turned on.
func Ready() (bool, string, error) {
	path, err := readyPath()
	if err != nil {
		return false, "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, strings.TrimSpace(string(data)), nil
}

// ClearReady turns the signal off. A starting session calls it in case a
// previous session failed to clean up.
func ClearReady() error {
	path, err := readyPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func readyPath() (string, error) {
	dir, err := RuntimeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ReadyFile), nil
}
