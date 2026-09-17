package systemd

import "testing"

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
