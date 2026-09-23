package x11

import "fmt"

// WindowManager es el gestor de ventanas que gobierna la pantalla.
type WindowManager struct {
	// Window es la ventana con la que se anuncia, que no se ve.
	Window Window
	// Name es lo que dice llamarse, si lo dice: "bspwm", "Xfwm4".
	Name string
}

func (wm *WindowManager) String() string {
	if wm.Name == "" {
		return fmt.Sprintf("window %#x", uint32(wm.Window))
	}
	return wm.Name
}

// Manager devuelve el gestor de ventanas de la pantalla, o nil si todavía no
// hay ninguno.
//
// Es la comprobación de EWMH: la ventana raíz tiene _NET_SUPPORTING_WM_CHECK
// apuntando a una ventana del gestor, y esa ventana tiene la misma propiedad
// apuntando a sí misma. Lo segundo hace falta porque la marca de la raíz
// sobrevive a un gestor de ventanas que muera de golpe; la suya, no, porque el
// servidor destruye sus ventanas al cerrarse su conexión.
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
		return nil, nil // marca de un gestor de ventanas que ya no está
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

// windowName lee el nombre de una ventana, primero el de EWMH y si no lo tiene
// el de siempre. Sólo sirve para decir en el diario quién ha contestado, así
// que una ventana sin nombre no es ningún error.
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
