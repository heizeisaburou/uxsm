package sessionentry

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/heizeisaburou/uxsm/internal/desktopentry"
	"github.com/heizeisaburou/uxsm/internal/session"
	"github.com/heizeisaburou/uxsm/internal/systemd"
)

// fromFile es la fuente de una de las entradas reales de testdata, copiadas de
// los paquetes de cada distribución con test/xsessions.sh.
func fromFile(t *testing.T, path string) *Source {
	t.Helper()
	e, err := desktopentry.Read(filepath.Join("testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	s, err := FromEntry(e, true)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestUxsm(t *testing.T) {
	tests := []struct {
		path  string
		opts  Options
		name  string
		exec  string
		names []string
	}{
		// Trae DesktopNames=: uxsm start los leerá de la entrada.
		{"arch/bspwm.desktop", Options{}, "bspwm (uxsm)", "uxsm start bspwm.desktop", []string{"bspwm"}},
		// La misma sin DesktopNames=: salen de la tabla y van con -D.
		{"ubuntu/bspwm.desktop", Options{}, "bspwm (uxsm)", "uxsm start -D bspwm bspwm.desktop", []string{"bspwm"}},
		// Sin la tabla serían "cinnamon-session-cinnamon", no lo que pone Cinnamon.
		// Y como Cinnamon lanza su propio autostart, la entrada lleva --no-autostart.
		{"arch/cinnamon.desktop", Options{}, "Cinnamon (uxsm)", "uxsm start --no-autostart -D X-Cinnamon cinnamon.desktop", []string{"X-Cinnamon"}},
		// Un gestor de ventanas dentro de un escritorio: el nombre es el del
		// escritorio, no el del script sawfish-mate-session, y el autostart lo
		// lanza el escritorio.
		{"fedora/sawfish-mate.desktop", Options{}, "Sawfish/MATE (uxsm)", "uxsm start --no-autostart -D MATE sawfish-mate.desktop", []string{"MATE"}},
		// -D añade al final, y sólo lo que no trae la entrada va en el Exec=.
		{"arch/bspwm.desktop", Options{Names: "Extra"}, "bspwm (uxsm)", "uxsm start -D Extra bspwm.desktop", []string{"bspwm", "Extra"}},
		// -e con todos los conocidos: no tira nada.
		{"arch/bspwm.desktop", Options{Names: "Extra:bspwm", Exclusive: true}, "bspwm (uxsm)", "uxsm start -e -D Extra:bspwm bspwm.desktop", []string{"Extra", "bspwm"}},
		// -N y -C sustituyen a los de la entrada.
		{"arch/bspwm.desktop", Options{Name: "Mine", Comment: "My bspwm"}, "Mine (uxsm)", "uxsm start bspwm.desktop", []string{"bspwm"}},
	}
	for _, tt := range tests {
		e, err := fromFile(t, tt.path).Uxsm(tt.opts)
		if err != nil {
			t.Errorf("%s %+v: %v", tt.path, tt.opts, err)
			continue
		}
		base := strings.TrimSuffix(filepath.Base(tt.path), ".desktop")
		if e.ID != base+"-uxsm.desktop" || e.Source != base+".desktop" || e.TryExec != "uxsm" {
			t.Errorf("%s: ID %q, Source %q, TryExec %q", tt.path, e.ID, e.Source, e.TryExec)
		}
		if e.Name != tt.name || e.Exec != tt.exec || !reflect.DeepEqual(e.DesktopNames, tt.names) {
			t.Errorf("%s %+v: Name %q, Exec %q, DesktopNames %q", tt.path, tt.opts, e.Name, e.Exec, e.DesktopNames)
		}
	}
}

func TestNames(t *testing.T) {
	// -e tira los conocidos y se queda con los de -D, como en uxsm start.
	e, err := fromFile(t, "arch/cinnamon.desktop").Uxsm(Options{Names: "Cinnamon", Exclusive: true})
	// Con -e los nombres son sólo los de -D, y "Cinnamon" a secas no está en la
	// tabla ―el de Cinnamon es "X-Cinnamon"―, así que no hay de dónde saber que
	// lanza su propio autostart y la entrada sale sin --no-autostart.
	if err != nil || e.Exec != "uxsm start -e -D Cinnamon cinnamon.desktop" {
		t.Errorf("-e dropping X-Cinnamon: %+v, %v", e, err)
	}
	// -e sin -D.
	if _, err := fromFile(t, "arch/bspwm.desktop").Uxsm(Options{Exclusive: true}); !errors.Is(err, ErrBadNames) {
		t.Errorf("-e without -D: %v, want ErrBadNames", err)
	}
	// Ni DesktopNames= ni en la tabla ni -D: no hay ningún nombre.
	if _, err := fromFile(t, "arch/notion.desktop").Uxsm(Options{}); !errors.Is(err, ErrNoNames) {
		t.Errorf("notion without -D: %v, want ErrNoNames", err)
	}
	if e, err := fromFile(t, "arch/notion.desktop").Uxsm(Options{Names: "notion"}); err != nil || e.Exec != "uxsm start -D notion notion.desktop" {
		t.Errorf("notion with -D: %+v, %v", e, err)
	}
	// Un -D mal escrito.
	if _, err := fromFile(t, "arch/bspwm.desktop").Uxsm(Options{Names: "a b"}); !errors.Is(err, ErrBadNames) {
		t.Errorf("-D with a space: %v, want ErrBadNames", err)
	}
}

func TestRefuses(t *testing.T) {
	e, err := desktopentry.Read("testdata/arch/qtile.desktop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromEntry(e, true); !errors.Is(err, ErrUsesSystemd) {
		t.Errorf("Arch's qtile.desktop: %v, want ErrUsesSystemd", err)
	}
	e, err = desktopentry.Read("testdata/debian/lightdm-xsession.desktop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromEntry(e, true); !errors.Is(err, ErrMetaSession) {
		t.Errorf("Debian's lightdm-xsession.desktop: %v, want ErrMetaSession", err)
	}
	// Lo que cuenta es la orden, no el nombre de la entrada.
	if _, err := FromEntry(&desktopentry.Entry{ID: "mine.desktop", Exec: "/usr/libexec/xinit-compat"}, true); !errors.Is(err, ErrMetaSession) {
		t.Errorf("an entry running xinit-compat: %v, want ErrMetaSession", err)
	}
	if _, err := FromEntry(&desktopentry.Entry{ID: "xinit-compat.desktop", Exec: "mywm"}, true); err != nil {
		t.Errorf("a desktop named xinit-compat.desktop: %v", err)
	}
	for _, e := range []desktopentry.Entry{
		{ID: "bspwm-uxsm.desktop", Exec: "bspwm"},
		{ID: "mine.desktop", Exec: "uxsm start bspwm.desktop"},
		{ID: "mine.desktop", Exec: "/usr/bin/uxsm start -D x bspwm.desktop"},
	} {
		if _, err := FromEntry(&e, true); !errors.Is(err, ErrUsesUxsm) {
			t.Errorf("%s with Exec=%s: %v, want ErrUsesUxsm", e.ID, e.Exec, err)
		}
	}
	for _, argv := range [][]string{{"uxsm", "start", "x.desktop"}, {"xinit-compat"}} {
		if _, err := FromCommand(argv, true); err == nil {
			t.Errorf("FromCommand(%q) should fail", argv)
		}
	}
	// Un ID que no vale como instancia de unidad no se podría arrancar.
	s, err := FromEntry(&desktopentry.Entry{ID: "my wm.desktop", Exec: "mywm"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Uxsm(Options{Names: "x"}); err == nil {
		t.Error("an ID with a space should be refused")
	}
	// Sólo se puede apuntar a una entrada que existe.
	if s, _ := FromTable("bspwm"); s != nil {
		if _, err := s.Uxsm(Options{}); err == nil {
			t.Error("Uxsm from the table should fail: there is no entry to point to")
		}
	}
}

func TestFromTable(t *testing.T) {
	s, err := FromTable("dwm")
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.UxsmExec(Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `[Desktop Entry]
# Generated by uxsm from its table of known desktops (dwm.desktop)
Type=Application
Name=dwm (uxsm)
Comment=dynamic window manager
Exec=uxsm start -D dwm -- dwm
TryExec=uxsm
DesktopNames=dwm;
X-UXSM-Source=dwm.desktop
`
	if got := string(e.Render()); got != want {
		t.Errorf("UxsmExec Render:\n%s\nwant:\n%s", got, want)
	}

	e, err = s.Plain(Options{})
	if err != nil {
		t.Fatal(err)
	}
	want = `[Desktop Entry]
# Generated by uxsm from its table of known desktops (dwm.desktop)
Type=Application
Name=dwm
Comment=dynamic window manager
Exec=dwm
TryExec=dwm
DesktopNames=dwm;
X-UXSM-Source=dwm.desktop
`
	if got := string(e.Render()); got != want {
		t.Errorf("Plain Render:\n%s\nwant:\n%s", got, want)
	}

	// Ni lo que no está en la tabla ni lo que está sin orden.
	for _, name := range []string{"nope", "gnome-classic-xorg"} {
		if _, err := FromTable(name); !errors.Is(err, ErrUnknown) {
			t.Errorf("FromTable(%q): %v, want ErrUnknown", name, err)
		}
	}
}

func TestFromCommand(t *testing.T) {
	// Un programa de la tabla: sus nombres cuentan como conocidos.
	s, err := FromCommand([]string{"/usr/bin/bspwm", "-c", "my config"}, true)
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.UxsmExec(Options{})
	if err != nil || e.ID != "bspwm-uxsm.desktop" || e.Exec != `uxsm start -D bspwm -- /usr/bin/bspwm -c "my config"` {
		t.Errorf("bspwm command: %+v, %v", e, err)
	}
	if e, err := s.UxsmExec(Options{Names: "other", Exclusive: true}); err != nil ||
		strings.Join(e.DesktopNames, ":") != "other" {
		t.Errorf("-e dropping bspwm: %+v, %v", e, err)
	}
	// Y sin tabla, ese mismo programa no tiene nombres conocidos: los pide.
	noTable, err := FromCommand([]string{"/usr/bin/bspwm"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := noTable.UxsmExec(Options{}); !errors.Is(err, ErrNoNames) {
		t.Errorf("bspwm without the table: %v, want ErrNoNames", err)
	}
	// Uno que no está: hacen falta los nombres.
	s, err = FromCommand([]string{"mywm"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Plain(Options{}); !errors.Is(err, ErrNoNames) {
		t.Errorf("mywm without -D: %v, want ErrNoNames", err)
	}
	e, err = s.Plain(Options{Names: "MyWM", Comment: "Mine"})
	if err != nil || e.ID != "mywm.desktop" || e.Name != "mywm" || e.Exec != "mywm" || e.Comment != "Mine" {
		t.Errorf("mywm plain: %+v, %v", e, err)
	}
}

// Lo que escribe Render lo tiene que leer igual desktopentry, que lee como un
// display manager: escapes, comillas, % y ";" dentro de un nombre incluidos.
func TestRenderRoundTrip(t *testing.T) {
	argv := []string{"/opt/my wm/bin", `quote"d`, `back\slash`, "100%", "$HOME", ""}
	e := &Entry{
		ID: "odd.desktop", Name: `Odd \ name`, Comment: "two\nlines",
		Exec: quoteExec(argv), TryExec: argv[0],
		DesktopNames: []string{"A;B", "C"}, Source: "odd.desktop", origin: "a test",
	}
	path := filepath.Join(t.TempDir(), e.ID)
	if err := os.WriteFile(path, e.Render(), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := desktopentry.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != e.Name || got.Comment != e.Comment || !reflect.DeepEqual(got.DesktopNames, e.DesktopNames) {
		t.Errorf("read back %+v", got)
	}
	back, err := desktopentry.SplitExec(got.Exec)
	if err != nil || !reflect.DeepEqual(back, argv) {
		t.Errorf("Exec %q read back as %q (%v), want %q", got.Exec, back, err, argv)
	}
}

// Todo lo de la tabla tiene que poder acabar en un Exec= con -D, y cada orden
// en un Exec= tal cual y como instancia de unidad en uxsm start.
func TestKnown(t *testing.T) {
	for id, k := range known {
		if err := systemd.CheckInstance(id); err != nil || !strings.HasSuffix(id, ".desktop") {
			t.Errorf("%s: not a usable entry ID", id)
		}
		if k.Name == "" || len(k.DesktopNames) == 0 || !session.ValidNames(strings.Join(k.DesktopNames, ":")) {
			t.Errorf("%s: %+v", id, k)
		}
		if k.Exec == "" {
			continue
		}
		// Sin comillas ni escapes: partida y vuelta a juntar, es la misma.
		argv, err := desktopentry.SplitExec(k.Exec)
		if err != nil || strings.Join(argv, " ") != k.Exec || quoteExec(argv) != k.Exec || strings.Contains(k.Exec, "/") {
			t.Errorf("%s: Exec %q needs quoting or has a path", id, k.Exec)
			continue
		}
		s, err := FromTable(id)
		if err != nil {
			t.Errorf("FromTable(%q): %v", id, err)
			continue
		}
		if _, err := s.UxsmExec(Options{}); err != nil {
			t.Errorf("%s: UxsmExec: %v", id, err)
		}
		if _, err := s.Plain(Options{}); err != nil {
			t.Errorf("%s: Plain: %v", id, err)
		}
	}
}
