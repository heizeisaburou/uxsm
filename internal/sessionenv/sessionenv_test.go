package sessionenv

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestComputeChanges(t *testing.T) {
	pre := []string{
		"HOME=/home/u",
		"PATH=/usr/bin",               // igual en post, pero está en alwaysExport
		"WAYLAND_DISPLAY=wayland-old", // alwaysUnset
		"XDG_SESSION_ID=7",            // alwaysUnset y neverExport
		"GONE=x",                      // no está en post
		"SSH_AUTH_SOCK=/run/ssh",      // neverCleanup
	}
	post := []string{
		"HOME=/home/u",
		"PATH=/usr/bin",
		"WAYLAND_DISPLAY=wayland-old",
		"XDG_SESSION_ID=9",
		"DISPLAY=:0",
		"NEW=1",
		"SHLVL=1", // neverExport
		"SSH_AUTH_SOCK=/run/other",
	}
	c := computeChanges(pre, post)

	wantSet := []string{"DISPLAY=:0", "NEW=1", "PATH=/usr/bin", "SSH_AUTH_SOCK=/run/other"}
	if !reflect.DeepEqual(c.set, wantSet) {
		t.Errorf("set = %q, want %q", c.set, wantSet)
	}
	wantUnset := []string{"GONE", "WAYLAND_DISPLAY", "XDG_SESSION_ID"}
	if !reflect.DeepEqual(c.unset, wantUnset) {
		t.Errorf("unset = %q, want %q", c.unset, wantUnset)
	}
	wantCleanup := []string{"DISPLAY", "NEW", "PATH"}
	if !reflect.DeepEqual(c.cleanup, wantCleanup) {
		t.Errorf("cleanup = %q, want %q", c.cleanup, wantCleanup)
	}
}

func TestCleanupNames(t *testing.T) {
	pre := []string{"PATH=/usr/bin", "DISPLAY=:0"} // DISPLAY viejo en la foto
	now := []string{"PATH=/usr/bin:/extra", "DISPLAY=:5", "NEW=1", "XDG_SESSION_TYPE=x11",
		"SSH_AUTH_SOCK=/run/ssh", "UNRELATED=1"}
	marked := []string{"PATH", "NEW", "DISPLAY", "SSH_AUTH_SOCK"}

	// PATH y DISPLAY estaban en la foto: no se borran, se restauran.
	// XDG_SESSION_TYPE no se apuntó, pero está en alwaysCleanup.
	// SSH_AUTH_SOCK está en neverCleanup. UNRELATED no es de la sesión.
	want := []string{"NEW", "XDG_SESSION_TYPE"}
	if got := cleanupNames(pre, now, marked); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseEnvDump(t *testing.T) {
	dump := []byte("A=1\x00__UXSM_MARK__=x\x00PWD=/tmp\x00MULTI=a\nb\x00")
	want := []string{"A=1", "MULTI=a\nb"}
	if got := parseEnvDump(dump); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSplitAtMark(t *testing.T) {
	msg, dump, err := splitAtMark([]byte("Sourcing x.\nMARKA=1\x00__UXSM_MARK__=MARK\x00"), "MARK")
	if err != nil || string(msg) != "Sourcing x.\n" || string(dump) != "A=1\x00__UXSM_MARK__=MARK\x00" {
		t.Errorf("got %q, %q, %v", msg, dump, err)
	}
	if _, _, err := splitAtMark([]byte("no mark"), "MARK"); err == nil {
		t.Error("output without the mark should be an error")
	}
}

// TestRunLoader ejecuta loader.sh de verdad, con un HOME y unos directorios XDG
// de prueba: comprueba el orden de carga de los ficheros de entorno y que la
// identidad llega al resultado.
func TestRunLoader(t *testing.T) {
	home, sys := t.TempDir(), t.TempDir()
	write := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// ORDER acumula el orden de carga.
	write(filepath.Join(sys, "uxsm/env"), `export ORDER="${ORDER}sys-env "`)
	write(filepath.Join(home, ".config/uxsm/env"), `export ORDER="${ORDER}home-env "`)
	write(filepath.Join(home, ".config/uxsm/env-testde"), `export ORDER="${ORDER}home-env-testde "`)
	write(filepath.Join(home, ".config/uxsm/env-second"), `export ORDER="${ORDER}home-env-second "`)
	write(filepath.Join(home, ".config/uxsm/env.d/10-a"), `export ORDER="${ORDER}home-env.d "`)
	write(filepath.Join(home, ".config/uxsm/env.d/20-b.disabled"), `export ORDER="${ORDER}disabled "`)
	write(filepath.Join(home, ".config/uxsm/env-other"), `export ORDER="${ORDER}other "`)
	write(filepath.Join(home, ".config/uxsm/env.d/30-broken"), `if then`)

	base := []string{
		"HOME=" + home,
		"PATH=/usr/bin:/bin",
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_CONFIG_DIRS=" + filepath.Join(sys, "etc-xdg-empty"),
		"XDG_DATA_DIRS=" + sys,
	}
	identity := []string{
		"XDG_CURRENT_DESKTOP=TestDE:Second",
		"XDG_SESSION_DESKTOP=TestDE",
		"XDG_MENU_PREFIX=testde-",
		"XDG_SESSION_TYPE=x11",
	}

	post, err := runLoader(base, identity)
	if err != nil {
		t.Fatal(err)
	}
	env := envMap(post)

	wantOrder := "sys-env home-env home-env.d home-env-testde home-env-second "
	if env["ORDER"] != wantOrder {
		t.Errorf("ORDER = %q, want %q", env["ORDER"], wantOrder)
	}
	for _, kv := range identity {
		name, value, _ := strings.Cut(kv, "=")
		if env[name] != value {
			t.Errorf("%s = %q, want %q", name, env[name], value)
		}
	}
	for name := range env {
		if strings.HasPrefix(name, auxPrefix) {
			t.Errorf("auxiliary variable %s leaked into the result", name)
		}
	}
}
