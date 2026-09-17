package main

import "testing"

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
