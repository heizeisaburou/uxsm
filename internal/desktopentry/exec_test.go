package desktopentry

import (
	"reflect"
	"testing"
)

func TestSplitExec(t *testing.T) {
	tests := []struct {
		exec string
		want []string
	}{
		{"bspwm", []string{"bspwm"}},
		{"startxfce4 --wayland", []string{"startxfce4", "--wayland"}},
		{"  a   b  ", []string{"a", "b"}},
		{`sh -c "echo hola mundo"`, []string{"sh", "-c", "echo hola mundo"}},
		{`sh -c "echo \"\$HOME\""`, []string{"sh", "-c", `echo "$HOME"`}},
		{`prog ""`, []string{"prog", ""}},
		{"prog %f %U", []string{"prog"}},
		{"prog 100%%", []string{"prog", "100%"}},
		// \s se deshace antes de separar: da dos argumentos, no uno con espacio.
		{`prog a\sb`, []string{"prog", "a", "b"}},
	}
	for _, tt := range tests {
		got, err := SplitExec(tt.exec)
		if err != nil {
			t.Errorf("SplitExec(%q) error: %v", tt.exec, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitExec(%q) = %q, want %q", tt.exec, got, tt.want)
		}
	}
}

func TestSplitExecErrors(t *testing.T) {
	for _, exec := range []string{"", "   ", `prog "unterminated`, "prog %z"} {
		if _, err := SplitExec(exec); err == nil {
			t.Errorf("SplitExec(%q) should fail", exec)
		}
	}
}
