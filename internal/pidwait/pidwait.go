// Package pidwait espera a que termine un proceso cualquiera, no sólo un hijo.
package pidwait

import (
	"errors"
	"fmt"
	"syscall"
)

// sysPidfdOpen es el número de la llamada pidfd_open(2), de Linux 5.3. El
// paquete syscall no le pone nombre en la mayoría de arquitecturas, pero las
// llamadas añadidas desde Linux 5.1 tienen el mismo número en todas.
const sysPidfdOpen = 434

// Wait bloquea hasta que termina el proceso pid. Si ya no existe, vuelve sin
// error.
//
// waitpid(2) sólo sirve con procesos hijos, y el que vigila uxsm es hijo del
// display manager. pidfd_open(2) da un descriptor de fichero para cualquier
// proceso, y ese descriptor se vuelve legible cuando el proceso termina: basta
// con esperar a que lo sea, sin preguntar en bucle. Es lo mismo que hace
// `uwsm aux waitpid` y el comando waitpid de util-linux, que Ubuntu 24.04 no
// trae.
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
		// FdSet guarda un bit por descriptor en palabras de 64 bits, que es
		// su tamaño en x86_64 y aarch64, las arquitecturas de los paquetes.
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
