package main

import (
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/heizeisaburou/uxsm/internal/session"
)

// TestDispatch verifies that a group dispatches consistently at every level:
// the subcommand receives the remaining arguments, and help, an empty command,
// or an unknown command are handled the same way.
func TestDispatch(t *testing.T) {
	// Group help goes to a disposable file, not the test output.
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

// TestAfterDashes verifies that arguments can be identified as coming after
// "--", which package flag silently consumes.
func TestAfterDashes(t *testing.T) {
	tests := []struct {
		args []string
		n    int
		want bool
	}{
		{[]string{"bspwm.desktop"}, 1, false},
		{[]string{"-D", "x", "bspwm.desktop"}, 1, false},
		{[]string{"--", "bspwm"}, 1, true},
		{[]string{"-e", "-D", "x", "--", "bspwm", "-c", "conf"}, 3, true},
		{[]string{"bspwm", "--", "x"}, 3, false},
	}
	for _, tt := range tests {
		if got := afterDashes(tt.args, tt.n); got != tt.want {
			t.Errorf("afterDashes(%q, %d) = %v, want %v", tt.args, tt.n, got, tt.want)
		}
	}
}

// TestResolveTarget verifies both forms of uxsm start: an entry, which is looked
// up in xsessions, and a command, which is not.
func TestResolveTarget(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_DATA_DIRS", dir)
	if err := os.MkdirAll(dir+"/xsessions", 0o755); err != nil {
		t.Fatal(err)
	}
	entry := "[Desktop Entry]\nName=Test\nExec=sh -c true\nDesktopNames=Test;\n"
	if err := os.WriteFile(dir+"/xsessions/test.desktop", []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		args    []string
		dashes  bool
		id      string
		argv    []string
		names   []string
		command bool
	}{
		// An entry: its Exec= and DesktopNames=.
		{[]string{"test.desktop"}, false, "test.desktop", []string{"sh", "-c", "true"}, []string{"Test"}, false},
		// A command, with or without "--": the instance is the program name.
		{[]string{"sh"}, false, "sh", []string{"sh"}, nil, true},
		{[]string{"sh", "-c", "true"}, true, "sh", []string{"sh", "-c", "true"}, nil, true},
		{[]string{"/bin/sh"}, true, "sh", []string{"/bin/sh"}, nil, true},
	}
	for _, tt := range tests {
		got, err := resolveTarget(tt.args, tt.dashes)
		if err != nil {
			t.Errorf("resolveTarget(%q, %v): %v", tt.args, tt.dashes, err)
			continue
		}
		want := &startTarget{id: tt.id, argv: tt.argv, names: tt.names, command: tt.command}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("resolveTarget(%q, %v) = %+v, want %+v", tt.args, tt.dashes, got, want)
		}
	}

	// After "--", a name ending in .desktop is a program, not an entry; neither
	// a missing entry nor a missing program is accepted.
	for _, bad := range []struct {
		args   []string
		dashes bool
	}{
		{[]string{"test.desktop"}, true},
		{[]string{"missing.desktop"}, false},
		{[]string{"uxsm-no-such-program"}, false},
	} {
		if _, err := resolveTarget(bad.args, bad.dashes); err == nil {
			t.Errorf("resolveTarget(%q, %v) should fail", bad.args, bad.dashes)
		}
	}
}

// TestWaitReadyAlreadyOn verifies the wait shortcut: if the desktop has already
// run uxsm finalize, the session is ready and there is nothing to wait for.
// Without that shortcut, the wait would try to contact the X server, which does
// not exist in this test.
func TestWaitReadyAlreadyOn(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("DISPLAY", "")

	if _, err := session.SignalReady("the desktop ran uxsm finalize"); err != nil {
		t.Fatalf("SignalReady: %v", err)
	}
	reason, err := waitReady(time.Second)
	if err != nil {
		t.Fatalf("waitReady: %v", err)
	}
	if reason != "the desktop ran uxsm finalize" {
		t.Errorf("waitReady says %q, want the reason of uxsm finalize", reason)
	}
}
