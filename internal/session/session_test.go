package session

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDesktopNames(t *testing.T) {
	tests := []struct {
		name string
		opts NamesOptions
		want []string
	}{
		{"entry only", NamesOptions{Entry: []string{"XFCE"}}, []string{"XFCE"}},
		{"current, entry and -D, in that order",
			NamesOptions{Current: "GNOME", Entry: []string{"Hyprland"}, Flag: "wlroots"},
			[]string{"GNOME", "Hyprland", "wlroots"}},
		{"duplicates keep the first appearance",
			NamesOptions{Current: "XFCE", Entry: []string{"XFCE", "GNOME"}, Flag: "GNOME:Extra"},
			[]string{"XFCE", "GNOME", "Extra"}},
		{"-e keeps only -D",
			NamesOptions{Current: "GNOME", Entry: []string{"Hyprland"}, Flag: "Hyprland", Exclusive: true},
			[]string{"Hyprland"}},
		{"executable as last resort", NamesOptions{Executable: "bspwm"}, []string{"bspwm"}},
		{"empty pieces are ignored", NamesOptions{Current: ":XFCE::"}, []string{"XFCE"}},
	}
	for _, tt := range tests {
		got, err := DesktopNames(tt.opts)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestDesktopNamesErrors(t *testing.T) {
	for name, opts := range map[string]NamesOptions{
		"-e without -D":    {Exclusive: true, Entry: []string{"XFCE"}},
		"malformed -D":     {Flag: "my desktop"},
		"empty name in -D": {Flag: "XFCE::GNOME"},
		"nothing at all":   {},
	} {
		_, err := DesktopNames(opts)
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
		// Sólo los errores de -D y -e son errores de argumentos.
		if isArgs := name != "nothing at all"; errors.Is(err, ErrBadNames) != isArgs {
			t.Errorf("%s: errors.Is(err, ErrBadNames) = %v, want %v", name, !isArgs, isArgs)
		}
	}
}

func TestIdentityVars(t *testing.T) {
	got := IdentityVars([]string{"XFCE", "GNOME"})
	want := []string{
		"XDG_CURRENT_DESKTOP=XFCE:GNOME",
		"XDG_SESSION_DESKTOP=XFCE",
		"XDG_MENU_PREFIX=xfce-",
		"XDG_SESSION_TYPE=x11",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFilterEnv(t *testing.T) {
	in := []string{"DISPLAY=:0", "_=/usr/bin/uxsm", "PWD=/home/u", "SHELL=/bin/zsh",
		"1BAD=x", "NO_EQUALS", "EMPTY=", "MULTI=a=b"}
	want := []string{"DISPLAY=:0", "EMPTY=", "MULTI=a=b"}
	if got := FilterEnv(in); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEnvFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uxsm", "env_login")
	env := []string{"A=1", "MULTILINE=line1\nline2", "SPACES=a b"}
	if err := WriteEnvFile(path, env); err != nil {
		t.Fatal(err)
	}
	got, err := ReadEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, env) {
		t.Errorf("got %q, want %q", got, env)
	}
}

func TestRuntimeDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if dir, err := RuntimeDir(); err != nil || dir != "/run/user/1000/uxsm" {
		t.Errorf("RuntimeDir() = %q, %v", dir, err)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if _, err := RuntimeDir(); err == nil {
		t.Error("RuntimeDir() without XDG_RUNTIME_DIR should fail")
	}
}

// TestReadySignal comprueba lo que hace que la señal sea una sola: la enciende
// quien llega primero, y quien llega después se la encuentra encendida.
func TestReadySignal(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	on, _, err := Ready()
	if err != nil || on {
		t.Fatalf("Ready with no session = %v, %v; want false, nil", on, err)
	}

	first, err := SignalReady("window manager ready: bspwm")
	if err != nil || !first {
		t.Fatalf("SignalReady = %v, %v; want true, nil", first, err)
	}

	second, err := SignalReady("the desktop ran uxsm finalize")
	if err != nil || second {
		t.Fatalf("the second SignalReady = %v, %v; want false, nil", second, err)
	}

	on, reason, err := Ready()
	if err != nil || !on {
		t.Fatalf("Ready = %v, %v; want true, nil", on, err)
	}
	if reason != "window manager ready: bspwm" {
		t.Errorf("Ready says %q, want the reason of the first one", reason)
	}

	if err := ClearReady(); err != nil {
		t.Fatalf("ClearReady: %v", err)
	}
	if on, _, _ := Ready(); on {
		t.Error("the signal is still on after ClearReady")
	}
	if err := ClearReady(); err != nil {
		t.Errorf("ClearReady on an already cleared signal: %v", err)
	}
}
