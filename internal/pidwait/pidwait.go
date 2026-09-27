// Package pidwait waits for any process to exit, not only a child process.
package pidwait

import (
	"errors"
	"fmt"
	"syscall"
)

// sysPidfdOpen is the syscall number for pidfd_open(2), introduced in Linux 5.3.
// Package syscall does not name it on most architectures, but syscalls added
// since Linux 5.1 use the same number on all architectures.
const sysPidfdOpen = 434

// Wait blocks until process pid exits. If it no longer exists, Wait returns
// without an error.
//
// waitpid(2) only works for child processes, while the process uxsm watches is
// a child of the display manager. pidfd_open(2) returns a file descriptor for
// any process, and that descriptor becomes readable when the process exits, so
// it can be awaited without polling. This is what `uwsm aux waitpid` and the
// util-linux waitpid command do; Ubuntu 24.04 does not ship the latter.
func Wait(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID %d", pid)
	}

	r, _, errno := syscall.Syscall(sysPidfdOpen, uintptr(pid), 0, 0)
	if errno == syscall.ESRCH {
		return nil
	}
	if errno != 0 {
		return fmt.Errorf("pidfd_open(%d): %w", pid, errno)
	}
	fd := int(r)
	defer syscall.Close(fd)

	for {
		// FdSet stores one bit per descriptor in 64-bit words, their size on
		// x86_64 and aarch64, the architectures targeted by the packages.
		var set syscall.FdSet
		set.Bits[fd/64] |= 1 << (uint(fd) % 64)

		_, err := syscall.Select(fd+1, &set, nil, nil, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("waiting for PID %d: %w", pid, err)
		}
		return nil
	}
}
