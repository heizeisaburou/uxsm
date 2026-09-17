package systemd

import "testing"

func TestUnitNames(t *testing.T) {
	if got := DesktopUnit("bspwm.desktop"); got != "uxsm-desktop@bspwm.desktop.service" {
		t.Errorf("DesktopUnit = %q", got)
	}
	if got := SessionTarget("bspwm.desktop"); got != "uxsm-session@bspwm.desktop.target" {
		t.Errorf("SessionTarget = %q", got)
	}
	if got := BindPIDUnit(1234); got != "uxsm-bindpid@1234.service" {
		t.Errorf("BindPIDUnit = %q", got)
	}
}

func TestCheckInstance(t *testing.T) {
	for _, id := range []string{"bspwm.desktop", "xfce-uxsm_test:1.desktop"} {
		if err := CheckInstance(id); err != nil {
			t.Errorf("CheckInstance(%q) = %v", id, err)
		}
	}
	for _, id := range []string{"my wm.desktop", "wm@1.desktop", "é.desktop"} {
		if err := CheckInstance(id); err == nil {
			t.Errorf("CheckInstance(%q) should fail", id)
		}
	}
}
