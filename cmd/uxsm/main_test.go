package main

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

// TestDispatch comprueba que un grupo reparte igual en cualquier nivel: la
// suborden recibe el resto de argumentos, y la ayuda, una orden vacía o una
// desconocida se tratan igual.
func TestDispatch(t *testing.T) {
	// La ayuda de los grupos va a un fichero descartable, no a la salida del test.
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = devnull, devnull
	defer func() { os.Stdout, os.Stderr = stdout, stderr }()

	var got []string
	g := group{name: "test", commands: []command{
		{"run", "", func(args []string) error { got = args; return nil }, false},
	}}

	if err := g.dispatch([]string{"run", "a", "-h"}); err != nil || !reflect.DeepEqual(got, []string{"a", "-h"}) {
		t.Errorf("dispatch(run a -h): err=%v, args=%q", err, got)
	}
	for _, help := range [][]string{{"-h"}, {"--help"}, {"help"}} {
		if err := g.dispatch(help); err != nil {
			t.Errorf("dispatch(%q) = %v, want nil", help, err)
		}
	}
	for _, bad := range [][]string{nil, {"missing"}} {
		if err := g.dispatch(bad); !errors.Is(err, errUsage) {
			t.Errorf("dispatch(%q) = %v, want errUsage", bad, err)
		}
	}
}

func TestWantsHelp(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"-h"}, true},
		{[]string{"bspwm.desktop", "--help"}, true},
		{[]string{"-e", "-D", "bspwm", "bspwm.desktop", "-help"}, true},
		{[]string{"bspwm.desktop"}, false},
		{[]string{"--", "kitty", "-h"}, false},
		{[]string{"-x", "--", "--help"}, false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := wantsHelp(tt.args); got != tt.want {
			t.Errorf("wantsHelp(%q) = %v, want %v", tt.args, got, tt.want)
		}
	}
}
