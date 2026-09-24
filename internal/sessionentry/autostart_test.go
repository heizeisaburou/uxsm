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
		{names: []string{"bspwm"}},                    // gestor de ventanas
		{names: []string{"i3"}},                       //
		{names: []string{"leftwm"}},                   // no está en la tabla: no se supone nada
		{names: []string{"XFCE"}, own: true},          // escritorio con gestor de sesión
		{names: []string{"xfce"}, own: true},          // sin distinguir mayúsculas
		{names: []string{"MATE"}, own: true},          // sawfish-mate acaba en mate-session
		{names: []string{"bspwm", "XFCE"}, own: true}, // basta uno
		{names: []string{"bspwm", "Custom"}},          // el desconocido no cuenta
	}
	for _, c := range cases {
		name, own := startsOwnAutostart(c.names)
		if own != c.own {
			t.Errorf("startsOwnAutostart(%v) = %q, %v; want %v", c.names, name, own, c.own)
		}
	}
}

// TestUxsmEntryNoAutostart comprueba lo que se apoya en la tabla: la entrada de
// un escritorio que lanza su propio autostart sale con --no-autostart, y la de
// un gestor de ventanas, sin ella.
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

// TestAutostartTable comprueba la marca de la tabla, que dice lo que la sesión
// hace y no lo que es: la llevan las que lanzan ellas mismas el autostart XDG,
// incluida Enlightenment, que es un gestor de ventanas; y no la llevan las que
// no lo lanzan, incluida icewm-session, que es una sesión con su script.
func TestAutostartTable(t *testing.T) {
	for id, k := range known {
		if k.OwnAutostart && len(k.DesktopNames) == 0 {
			t.Errorf("%s starts its own XDG autostart but has no DesktopNames to recognise it by", id)
		}
	}
	for _, id := range []string{"xfce.desktop", "plasmax11.desktop", "gnome-xorg.desktop",
		"sawfish-mate.desktop", "enlightenment.desktop"} {
		if !known[id].OwnAutostart {
			t.Errorf("%s should be marked as starting its own XDG autostart", id)
		}
	}
	for _, id := range []string{"bspwm.desktop", "i3.desktop", "icewm-session.desktop"} {
		if known[id].OwnAutostart {
			t.Errorf("%s should not be marked as starting its own XDG autostart", id)
		}
	}
}
