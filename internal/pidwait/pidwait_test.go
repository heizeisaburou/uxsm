package pidwait

import (
	"os/exec"
	"testing"
	"time"
)

// TestWaitRunning lanza un proceso que dura 300 ms y comprueba que Wait no
// vuelve antes de que termine.
func TestWaitRunning(t *testing.T) {
	cmd := exec.Command("sleep", "0.3")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()

	start := time.Now()
	if err := Wait(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Errorf("Wait returned after %v, before the process ended", elapsed)
	}
}

// TestWaitGone comprueba que un proceso que ya no existe no es un error.
func TestWaitGone(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := Wait(cmd.Process.Pid); err != nil {
		t.Errorf("Wait on a finished process: %v", err)
	}
}

func TestWaitInvalid(t *testing.T) {
	if err := Wait(0); err == nil {
		t.Error("Wait(0) should fail")
	}
}
