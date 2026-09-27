package sessionentry

import (
	"strings"
	"testing"
)

func TestStartsOwnAutostart(t *testing.T) {
	cases := []struct {
		names []string
		own   bool
	}{
		{names: []string{"bspwm"}},                    // window manager
		{names: []string{"i3"}},                       //
		{names: []string{"leftwm"}},                   // absent from the table: assume nothing
		{names: []string{"XFCE"}, own: true},          // desktop with a session manager
		{names: []string{"xfce"}, own: true},          // case-insensitive
		{names: []string{"MATE"}, own: true},          // sawfish-mate ends in mate-session
		{names: []string{"bspwm", "XFCE"}, own: true}, // one is enough
		{names: []string{"bspwm", "Custom"}},          // an unknown name does not count
	}
	for _, c := range cases {
		name, own := startsOwnAutostart(c.names)
		if own != c.own {
			t.Errorf("startsOwnAutostart(%v) = %q, %v; want %v", c.names, name, own, c.own)
		}
	}
}

// TestUxsmEntryNoAutostart verifies table-dependent behavior: an entry for a
// desktop that launches its own autostart includes --no-autostart, while a
// window manager entry does not.
func TestUxsmEntryNoAutostart(t *testing.T) {
	for _, c := range []struct {
		desktop string
		flag    bool
	}{
		{desktop: "xfce", flag: true},
		{desktop: "mate", flag: true},
		{desktop: "bspwm"},
		{desktop: "i3"},
	} {
		src, err := FromTable(c.desktop)
		if err != nil {
			t.Fatalf("FromTable(%q): %v", c.desktop, err)
		}
		entry, err := src.UxsmExec(Options{})
		if err != nil {
			t.Fatalf("UxsmExec(%q): %v", c.desktop, err)
		}
		if got := strings.Contains(entry.Exec, noAutostartFlag); got != c.flag {
			t.Errorf("the entry of %s has Exec=%q; want --no-autostart: %v", c.desktop, entry.Exec, c.flag)
		}
	}
}

// TestAutostartTable verifies the table marker, which describes what a session
// does rather than what it is: sessions that launch XDG autostart themselves
// have it, while others do not, even if they have their own startup file like
// icewm-session or form a complete desktop like Enlightenment.
func TestAutostartTable(t *testing.T) {
	for id, k := range known {
		if k.OwnAutostart && len(k.DesktopNames) == 0 {
			t.Errorf("%s starts its own XDG autostart but has no DesktopNames to recognise it by", id)
		}
	}
	for _, id := range []string{"xfce.desktop", "plasmax11.desktop", "gnome-xorg.desktop",
		"sawfish-mate.desktop"} {
		if !known[id].OwnAutostart {
			t.Errorf("%s should be marked as starting its own XDG autostart", id)
		}
	}
	for _, id := range []string{"bspwm.desktop", "i3.desktop", "icewm-session.desktop",
		"enlightenment.desktop"} {
		if known[id].OwnAutostart {
			t.Errorf("%s should not be marked as starting its own XDG autostart", id)
		}
	}
}
