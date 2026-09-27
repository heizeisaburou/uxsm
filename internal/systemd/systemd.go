// Package systemd communicates with the user systemd manager (systemd --user).
//
// For now it invokes systemctl and busctl, just like manual checks: this is the
// easiest approach to follow and requires no D-Bus client.
package systemd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// DesktopUnit is the unit that runs a session entry's desktop: an instance of
// the uxsm-desktop@.service template.
//
// id is the entry ID, its file name including .desktop, as received by
// `uxsm start`: for /usr/share/xsessions/bspwm.desktop, id is "bspwm.desktop"
// and the unit is "uxsm-desktop@bspwm.desktop.service". If the session starts
// from a command (`uxsm start -- bspwm`), id is the program name:
// "uxsm-desktop@bspwm.service".
func DesktopUnit(id string) string {
	return "uxsm-desktop@" + id + ".service"
}

// SessionTarget is an entry's session target:
// "uxsm-session@bspwm.desktop.target" for "bspwm.desktop". While it is active,
// graphical-session.target is active too.
func SessionTarget(id string) string {
	return "uxsm-session@" + id + ".target"
}

// AutostartTarget is a session's XDG autostart target:
// "uxsm-autostart@bspwm.desktop.target". Starting it pulls in systemd's
// standard target, the only mechanism allowed to start it because that target
// has RefuseManualStart=.
func AutostartTarget(id string) string {
	return "uxsm-autostart@" + id + ".target"
}

// BindPIDUnit is the unit that watches session process pid and stops the session
// when it exits: "uxsm-bindpid@1234.service".
func BindPIDUnit(pid int) string {
	return "uxsm-bindpid@" + strconv.Itoa(pid) + ".service"
}

// ShutdownTarget is the target that stops the session: starting it makes systemd
// stop everything that conflicts with it (Conflicts=).
const ShutdownTarget = "uxsm-shutdown.target"

// CheckInstance verifies that id can be used verbatim as a unit instance.
//
// systemd unit names accept only letters, digits, ":", "-", "_", and ".". An
// ID with other characters would need escaping (systemd-escape); it is rejected
// for now because real session entries do not use such characters.
func CheckInstance(id string) error {
	for _, r := range id {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune(":-_.", r)
		if !ok {
			return fmt.Errorf("%q cannot be used as a unit instance: character %q is not allowed", id, r)
		}
	}
	return nil
}

// Environment returns the manager environment in "NAME=value" form.
//
// It requests the environment over D-Bus with busctl, which returns each
// variable verbatim in JSON. `systemctl show-environment` is not used because it
// escapes some values as $'…', which would require manual decoding.
func Environment() ([]string, error) {
	out, err := exec.Command("busctl", "--user", "--json=short", "get-property",
		"org.freedesktop.systemd1", "/org/freedesktop/systemd1",
		"org.freedesktop.systemd1.Manager", "Environment").Output()
	if err != nil {
		return nil, fmt.Errorf("reading the systemd user environment: %w", err)
	}
	var reply struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(out, &reply); err != nil {
		return nil, fmt.Errorf("parsing the systemd user environment: %w", err)
	}
	return reply.Data, nil
}

// UnsetEnvironment removes variables names from the manager.
func UnsetEnvironment(names ...string) error {
	if len(names) == 0 {
		return nil
	}
	args := append([]string{"--user", "unset-environment"}, names...)
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// DBusIsBroker reports whether the session bus uses dbus-broker.
//
// uxsm needs this because systemd and D-Bus may maintain separate activation
// environments.
//
// With dbus-broker, service activation is delegated to systemd. `systemd --user`
// ultimately executes the process, so it directly receives the manager
// environment and no separate environment needs to be maintained.
//
// With dbus-daemon, D-Bus may execute the service itself. In that case it uses
// its own activation environment, independent of systemd's, and uxsm must also
// update it through UpdateDBusActivationEnvironment.
//
// "May" matters because this depends on each service's activation: if its D-Bus
// file declares SystemdService= and dbus-daemon has systemd activation enabled,
// it delegates startup to `systemd --user`. Otherwise dbus-daemon directly runs
// Exec= from the D-Bus file.
//
// Like uwsm, uxsm distinguishes them by checking which unit `dbus.service`
// resolves to.
func DBusIsBroker() bool {
	out, err := exec.Command("systemctl", "--user", "show", "-p", "Id", "--value", "dbus.service").Output()
	return err == nil && strings.TrimSpace(string(out)) == "dbus-broker.service"
}

// UpdateDBusActivationEnvironment sets vars, in "NAME=value" form, in the
// dbus-daemon activation environment. D-Bus cannot remove variables, so
// "removing" one means setting it to an empty value.
func UpdateDBusActivationEnvironment(vars []string) error {
	if len(vars) == 0 {
		return nil
	}
	args := []string{"--user", "call", "org.freedesktop.DBus", "/org/freedesktop/DBus",
		"org.freedesktop.DBus", "UpdateActivationEnvironment", "a{ss}", strconv.Itoa(len(vars))}
	for _, kv := range vars {
		name, value, _ := strings.Cut(kv, "=")
		args = append(args, name, value)
	}
	cmd := exec.Command("busctl", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// SetEnvironment sets vars in the manager in "NAME=value" form. Values are
// passed to systemctl as arguments without a shell, so spaces and quotes need
// no escaping.
func SetEnvironment(vars ...string) error {
	if len(vars) == 0 {
		return nil
	}
	args := append([]string{"--user", "set-environment"}, vars...)
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// DaemonReload asks the manager to reread its units. This is required after
// writing or removing a drop-in from the runtime unit directory because
// drop-ins are read when a unit is loaded, not when it starts.
func DaemonReload() error {
	return systemctl("daemon-reload")
}

// LiveUnits returns units matching patterns that have not finished: they are
// active, activating, reloading, or deactivating.
//
// A `deactivating` unit remains live until systemd completes its entire stop
// sequence, including ExecStopPost=.
func LiveUnits(patterns ...string) ([]string, error) {
	args := append([]string{"--user", "list-units", "--all", "--plain", "--no-legend",
		"--state=active,activating,deactivating,reloading"}, patterns...)
	out, err := exec.Command("systemctl", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("listing systemd user units: %w", err)
	}
	var units []string
	for _, line := range strings.Split(string(out), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			units = append(units, fields[0])
		}
	}
	return units, nil
}

// Start starts unit and waits for systemd to finish the start job.
func Start(unit string) error {
	return systemctl("start", unit)
}

// StartNoBlock starts unit without waiting for systemd to finish the job. This
// is required from inside another unit, as in the desktop's ExecStartPost=:
// waiting there for a systemd job can deadlock both jobs.
func StartNoBlock(unit string) error {
	return systemctl("start", "--no-block", unit)
}

// systemctl runs a systemctl command against the user manager and passes its
// output through to uxsm's output.
func systemctl(args ...string) error {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// CheckUserBus verifies that the D-Bus session bus exists. busctl and
// ExecStartWait require it: `systemctl --user start --wait` waits over D-Bus for
// the unit to finish, and without a bus it fails with a message that does not
// identify what is missing ("Failed to connect to user scope bus via local
// transport"). Other systemctl commands work without it through the manager's
// private socket.
//
// It looks where systemctl looks: the DBUS_SESSION_BUS_ADDRESS path for a
// unix:path=… address, or $XDG_RUNTIME_DIR/bus if the variable is unset. Other
// address forms cannot be checked and are accepted.
func CheckUserBus() error {
	path := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "bus")
	if addr := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); addr != "" {
		rest, ok := strings.CutPrefix(addr, "unix:path=")
		if !ok {
			return nil
		}
		path, _, _ = strings.Cut(rest, ",")
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Type() != os.ModeSocket {
		return fmt.Errorf("no D-Bus session bus at %s: uxsm needs the session bus of systemd --user "+
			"(on Debian and Ubuntu, the dbus-user-session package)", path)
	}
	return nil
}

// ExecStartWait replaces the current process with:
//
//	systemctl --user start --wait unit
//
// `syscall.Exec` preserves the PID, so the display manager keeps watching the
// same process, which is now `systemctl`. It waits for the unit to finish; when
// that happens, the session process also exits.
//
// If `exec` succeeds, this function does not return. It returns an error only
// when `systemctl` cannot be executed.
func ExecStartWait(unit string) error {
	path, err := exec.LookPath("systemctl")
	if err != nil {
		return err
	}
	argv := []string{"systemctl", "--user", "start", "--wait", unit}
	return syscall.Exec(path, argv, os.Environ())
}
