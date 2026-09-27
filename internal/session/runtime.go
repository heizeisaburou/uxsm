package session

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// These files live in RuntimeDir and pass the environment, and for a command
// also its arguments, from `uxsm start` to systemd services that run later and
// do not directly inherit the display manager's environment.
//
// This follows uwsm's mechanism: upstream also stores the login environment in
// `env_login` inside its runtime directory. uxsm adds `env_identity` to keep
// identity variables computed by start separate.
//
// The environment service also creates its own state files so it can restore
// and clean the environment when the session ends.
const (
	// LoginFile is the environment with which the display manager launched the
	// session, filtered by FilterEnv (defined in this file).
	LoginFile = "env_login"
	// IdentityFile contains the identity variables computed by uxsm start: the
	// result of IdentityVars (defined in identity.go).
	IdentityFile = "env_identity"
	// AutostartFile marks that this session launches XDG autostart. uxsm start
	// writes it when uxsm is responsible for autostart, and `uxsm aux autostart`
	// checks it and starts nothing if absent. Its content records the reason for
	// the journal.
	AutostartFile = "autostart"
	// CommandFile contains the desktop command line when the session starts from
	// a command (`uxsm start -- bspwm`) instead of an entry. uxsm aux exec reads
	// it here. It uses the same format as environment files: arguments separated
	// by null bytes.
	CommandFile = "command"
)

// RuntimeDir is $XDG_RUNTIME_DIR/uxsm, the session's working directory.
//
// logind creates XDG_RUNTIME_DIR when opening a session and removes it after the
// last one closes, so anything left here does not outlive the user session.
func RuntimeDir() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(base) {
		return "", errors.New("XDG_RUNTIME_DIR is not set to an absolute path")
	}
	return filepath.Join(base, "uxsm"), nil
}

// varName matches a shell-valid environment variable name.
var varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// dropVars are shell- or process-specific variables, not session variables.
var dropVars = map[string]bool{"_": true, "SHELL": true, "PWD": true, "OLDPWD": true}

// FilterEnv retains session-related "NAME=value" assignments: it removes
// assignments without a valid name and shell-specific variables (_, SHELL,
// PWD, OLDPWD). This is the same filter uwsm applies to its login environment.
func FilterEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		name, _, ok := strings.Cut(kv, "=")
		if ok && varName.MatchString(name) && !dropVars[name] {
			out = append(out, kv)
		}
	}
	return out
}

// WriteEnvFile stores env at path as null-separated assignments; a null byte is
// the only character that cannot appear in a value.
//
// It writes a temporary file first and renames it so readers never see a
// partial file. Only the user can read it.
func WriteEnvFile(path string, env []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	data := strings.Join(env, "\x00")
	if err := os.WriteFile(tmp, []byte(data), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadEnvFile reads a file written by WriteEnvFile.
func ReadEnvFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	return strings.Split(string(data), "\x00"), nil
}
