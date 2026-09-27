package x11

import "fmt"

// WindowManager is the window manager controlling the screen.
type WindowManager struct {
	// Window is the invisible window through which it announces itself.
	Window Window
	// Name is the name it reports, if any: "bspwm", "Xfwm4".
	Name string
}

func (wm *WindowManager) String() string {
	if wm.Name == "" {
		return fmt.Sprintf("window %#x", uint32(wm.Window))
	}
	return wm.Name
}

// Manager returns the screen's window manager, or nil if none is present yet.
//
// This is the EWMH check: the root window has _NET_SUPPORTING_WM_CHECK pointing
// to a manager window, and that window has the same property pointing to itself.
// The second check is necessary because the root marker survives a window
// manager that exits abruptly, while its own marker does not because the server
// destroys its windows when the connection closes.
func (c *Conn) Manager() (*WindowManager, error) {
	check, err := c.Atom("_NET_SUPPORTING_WM_CHECK")
	if err != nil {
		return nil, err
	}
	if check == 0 {
		return nil, nil
	}

	value, err := c.Property(c.root, check, atomWindow, 1)
	if err != nil || len(value) < 4 {
		return nil, err
	}
	win := Window(le.Uint32(value[:4]))

	value, err = c.Property(win, check, atomWindow, 1)
	if isError(err, BadWindow) {
		return nil, nil // marker from a window manager that is no longer present
	}
	if err != nil || len(value) < 4 || Window(le.Uint32(value[:4])) != win {
		return nil, err
	}

	name, err := c.windowName(win)
	if err != nil {
		return nil, err
	}
	return &WindowManager{Window: win, Name: name}, nil
}

// windowName reads a window's name, preferring the EWMH name and falling back
// to the traditional one. It is only used to identify the responder in the
// journal, so an unnamed window is not an error.
func (c *Conn) windowName(w Window) (string, error) {
	utf8, err := c.Atom("UTF8_STRING")
	if err != nil {
		return "", err
	}
	if utf8 != 0 {
		netWMName, err := c.Atom("_NET_WM_NAME")
		if err != nil {
			return "", err
		}
		if netWMName != 0 {
			value, err := c.Property(w, netWMName, utf8, 64)
			if isError(err, BadWindow) {
				return "", nil
			}
			if err != nil {
				return "", err
			}
			if len(value) > 0 {
				return string(value), nil
			}
		}
	}

	value, err := c.Property(w, atomWMName, atomString, 64)
	if isError(err, BadWindow) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(value), nil
}
