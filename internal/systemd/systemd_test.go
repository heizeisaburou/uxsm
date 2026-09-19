package systemd

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestUnitNames(t *testing.T) {
	if got := DesktopUnit("bspwm.desktop"); got != "uxsm-desktop@bspwm.desktop.service" {
		t.Errorf("DesktopUnit = %q", got)
	}
	if got := SessionTarget("bspwm.desktop"); got != "uxsm-session@bspwm.desktop.target" {
		t.Errorf("SessionTarget = %q", got)
	}
	if got := BindPIDUnit(1234); got != "uxsm-bindpid@1234.service" {
		t.Errorf("BindPIDUnit = %q", got)
	}
}

func TestCheckInstance(t *testing.T) {
	for _, id := range []string{"bspwm.desktop", "xfce-uxsm_test:1.desktop"} {
		if err := CheckInstance(id); err != nil {
			t.Errorf("CheckInstance(%q) = %v", id, err)
		}
	}
	for _, id := range []string{"my wm.desktop", "wm@1.desktop", "é.desktop"} {
		if err := CheckInstance(id); err == nil {
			t.Errorf("CheckInstance(%q) should fail", id)
		}
	}
}

func TestCheckUserBus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	if err := CheckUserBus(); err == nil {
		t.Error("CheckUserBus without a bus should fail")
	}

	// Un fichero normal no es un bus.
	if err := os.WriteFile(filepath.Join(dir, "bus"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckUserBus(); err == nil {
		t.Error("CheckUserBus with a regular file as bus should fail")
	}
	os.Remove(filepath.Join(dir, "bus"))

	l, err := net.Listen("unix", filepath.Join(dir, "bus"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := CheckUserBus(); err != nil {
		t.Errorf("CheckUserBus with $XDG_RUNTIME_DIR/bus = %v", err)
	}

	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(dir, "other")+",guid=0123")
	if err := CheckUserBus(); err == nil {
		t.Error("CheckUserBus should look at the path of DBUS_SESSION_BUS_ADDRESS")
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(dir, "bus"))
	if err := CheckUserBus(); err != nil {
		t.Errorf("CheckUserBus with DBUS_SESSION_BUS_ADDRESS = %v", err)
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:abstract=/tmp/dbus-x")
	if err := CheckUserBus(); err != nil {
		t.Errorf("CheckUserBus with an abstract address = %v", err)
	}
}
