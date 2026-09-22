package sessionentry

import (
	"strings"
	"testing"
)

func TestAutostart(t *testing.T) {
	cases := []struct {
		names  []string
		start  bool
		reason string
	}{
		{names: []string{"bspwm"}, start: true, reason: "window manager"},
		{names: []string{"i3"}, start: true, reason: "window manager"},
		{names: []string{"XFCE"}, reason: "its own"},
		{names: []string{"xfce"}, reason: "its own"},          // sin distinguir mayúsculas
		{names: []string{"MATE"}, reason: "its own"},          // sawfish-mate acaba en mate-session
		{names: []string{"bspwm", "XFCE"}, reason: "its own"}, // basta uno
		{names: []string{"leftwm"}, reason: "does not know"},  // no está en la tabla
		{names: []string{"bspwm", "Custom"}, reason: "does not know"},
	}
	for _, c := range cases {
		start, reason := Autostart(c.names)
		if start != c.start {
			t.Errorf("Autostart(%v) = %v, want %v (%s)", c.names, start, c.start, reason)
		}
		if !strings.Contains(reason, c.reason) {
			t.Errorf("Autostart(%v) said %q, want something about %q", c.names, reason, c.reason)
		}
	}
}

// TestAutostartTable comprueba que la tabla distingue las dos clases: los
// gestores de ventanas la llevan puesta y los escritorios no.
func TestAutostartTable(t *testing.T) {
	for id, k := range known {
		if k.WindowManager && len(k.DesktopNames) == 0 {
			t.Errorf("%s is marked as a window manager but has no DesktopNames", id)
		}
	}
	for _, id := range []string{"bspwm.desktop", "i3.desktop", "icewm-session.desktop"} {
		if !known[id].WindowManager {
			t.Errorf("%s should be a window manager", id)
		}
	}
	for _, id := range []string{"xfce.desktop", "plasmax11.desktop", "gnome-xorg.desktop", "sawfish-mate.desktop"} {
		if known[id].WindowManager {
			t.Errorf("%s should not be a window manager: it runs a session manager", id)
		}
	}
}
