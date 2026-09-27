package desktopentry

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	const content = `# comentario
[Desktop Entry]
Type=Application
Name = Xfce\sSession
Name[es]=Sesión de Xfce
Exec=startxfce4
DesktopNames=XFCE;GNOME\;Legacy;

[Desktop Action other]
Exec=other
`
	e, err := parse(strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "Xfce Session" {
		t.Errorf("Name = %q", e.Name)
	}
	if e.Exec != "startxfce4" {
		t.Errorf("Exec = %q, the action's Exec must not win", e.Exec)
	}
	if want := []string{"XFCE", "GNOME;Legacy"}; !reflect.DeepEqual(e.DesktopNames, want) {
		t.Errorf("DesktopNames = %q, want %q", e.DesktopNames, want)
	}
}

func TestParseWithoutExec(t *testing.T) {
	if _, err := parse(strings.NewReader("[Desktop Entry]\nName=x\n")); err == nil {
		t.Error("an entry without Exec= must be an error")
	}
}

func TestCheckID(t *testing.T) {
	for _, id := range []string{"bspwm.desktop", "xfce-uxsm.desktop"} {
		if err := checkID(id); err != nil {
			t.Errorf("checkID(%q) = %v", id, err)
		}
	}
	for _, id := range []string{"bspwm", ".desktop", "../x.desktop", "a/b.desktop"} {
		if err := checkID(id); err == nil {
			t.Errorf("checkID(%q) should fail", id)
		}
	}
}

// TestFindOrder verifies that the XDG_DATA_HOME entry shadows the system entry.
func TestFindOrder(t *testing.T) {
	home, system := t.TempDir(), t.TempDir()
	write := func(dir, exec string) {
		d := filepath.Join(dir, "xsessions")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "[Desktop Entry]\nExec=" + exec + "\n"
		if err := os.WriteFile(filepath.Join(d, "test.desktop"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(home, "from-home")
	write(system, "from-system")
	t.Setenv("XDG_DATA_HOME", home)
	t.Setenv("XDG_DATA_DIRS", system)

	e, err := Find("xsessions", "test.desktop")
	if err != nil {
		t.Fatal(err)
	}
	if e.Exec != "from-home" {
		t.Errorf("Exec = %q, want the one from XDG_DATA_HOME", e.Exec)
	}

	if _, err := Find("xsessions", "missing.desktop"); err == nil {
		t.Error("a missing entry must be an error")
	}
}
