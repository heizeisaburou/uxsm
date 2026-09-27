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

// display is a parsed DISPLAY: "localhost:10.1" is screen 1 of display 10 on
// machine localhost.
type display struct {
	// display is the original value, used in error messages.
	display string
	host    string
	number  int
	screen  int
}

// parseDisplay parses a DISPLAY in "[host]:number[.screen]" form. If s is empty,
// it uses the DISPLAY variable.
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

// local says whether the display belongs to this machine and is therefore
// reached through a Unix socket. "localhost" is not local in this sense: with
// DISPLAY forwarded over SSH, the server is across a network socket.
func (d display) local() bool { return d.host == "" || d.host == "unix" }

// socketDir is where local X servers keep their sockets.
const socketDir = "/tmp/.X11-unix"

// dial opens a connection to the display's X server.
func dial(d display) (net.Conn, error) {
	if !d.local() {
		return net.Dial("tcp", net.JoinHostPort(d.host, strconv.Itoa(6000+d.number)))
	}

	path := filepath.Join(socketDir, fmt.Sprintf("X%d", d.number))
	nc, err := net.Dial("unix", path)
	if err == nil {
		return nc, err
	}
	// Linux X servers also listen on an abstract socket in the kernel namespace
	// under the same name. If the filesystem socket is absent—for a server
	// started with -nolisten unix or in another mount namespace—that socket
	// remains available.
	if abstract, aerr := net.Dial("unix", "@"+path); aerr == nil {
		return abstract, nil
	}
	return nil, err
}
