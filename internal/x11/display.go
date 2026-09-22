package x11

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// display es un DISPLAY ya desmenuzado: "localhost:10.1" es la pantalla 1 del
// display 10 de la máquina localhost.
type display struct {
	// display es el valor original, para los mensajes de error.
	display string
	host    string
	number  int
	screen  int
}

// parseDisplay desmenuza un DISPLAY, "[host]:número[.pantalla]". Con s vacía usa
// la variable DISPLAY.
func parseDisplay(s string) (display, error) {
	if s == "" {
		s = os.Getenv("DISPLAY")
	}
	if s == "" {
		return display{}, errors.New("DISPLAY is not set")
	}

	d := display{display: s}
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return display{}, fmt.Errorf("DISPLAY=%s has no display number", s)
	}
	d.host = s[:i]

	number, screen, hasScreen := strings.Cut(s[i+1:], ".")
	var err error
	if d.number, err = strconv.Atoi(number); err != nil || d.number < 0 {
		return display{}, fmt.Errorf("DISPLAY=%s has no display number", s)
	}
	if hasScreen {
		if d.screen, err = strconv.Atoi(screen); err != nil || d.screen < 0 {
			return display{}, fmt.Errorf("DISPLAY=%s has a bad screen number", s)
		}
	}
	return d, nil
}

// local dice si el display es de esta máquina, y entonces se llega a él por un
// socket de unix. "localhost" no lo es: con un DISPLAY reenviado por ssh, el
// servidor está al otro lado de un socket de red.
func (d display) local() bool { return d.host == "" || d.host == "unix" }

// socketDir es donde los servidores X de la máquina tienen su socket.
const socketDir = "/tmp/.X11-unix"

// dial abre la conexión con el servidor X del display.
func dial(d display) (net.Conn, error) {
	if !d.local() {
		return net.Dial("tcp", net.JoinHostPort(d.host, strconv.Itoa(6000+d.number)))
	}

	path := filepath.Join(socketDir, fmt.Sprintf("X%d", d.number))
	nc, err := net.Dial("unix", path)
	if err == nil {
		return nc, err
	}
	// Los servidores X de Linux escuchan además en un socket abstracto, del
	// espacio de nombres del núcleo, con el mismo nombre. Si el del sistema
	// de ficheros no está ―un servidor arrancado con -nolisten unix, o dentro
	// de otro espacio de nombres de montaje―, queda ése.
	if abstract, aerr := net.Dial("unix", "@"+path); aerr == nil {
		return abstract, nil
	}
	return nil, err
}
