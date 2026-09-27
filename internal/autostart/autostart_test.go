package autostart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heizeisaburou/uxsm/internal/appunit"
)

func TestWriteAndRemove(t *testing.T) {
	run := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", run)

	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join(run, "systemd/user/app-@autostart.service.d/uxsm-tweaks.conf")
	if path != want {
		t.Errorf("Path() = %q, want %q", path, want)
	}

	if err := Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the drop-in: %v", err)
	}
	if !strings.Contains(string(data), "Slice="+appunit.AppSlice) {
		t.Errorf("the drop-in does not put the entries in %s:\n%s", appunit.AppSlice, data)
	}
	if !strings.Contains(string(data), "PartOf=xdg-desktop-autostart.target") {
		t.Errorf("the drop-in does not tie the entries to their target:\n%s", data)
	}

	// Writing it twice is not an error: the session may start again.
	if err := Write(); err != nil {
		t.Fatalf("Write twice: %v", err)
	}

	if err := Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the drop-in is still there after Remove: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Error("the drop-in directory was left behind, and it was empty")
	}
	// Neither is removing it when absent: every session calls this while stopping.
	if err := Remove(); err != nil {
		t.Errorf("Remove without a drop-in: %v", err)
	}

	// If someone else left something inside, the directory remains.
	if err := Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	other := filepath.Join(filepath.Dir(path), "slice-tweak.conf")
	if err := os.WriteFile(other, []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(); err != nil {
		t.Fatalf("Remove with another drop-in there: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("Remove took somebody else's drop-in with it: %v", err)
	}

	t.Setenv("XDG_RUNTIME_DIR", "")
	if _, err := Path(); err == nil {
		t.Error("Path() without XDG_RUNTIME_DIR should fail")
	}
}
