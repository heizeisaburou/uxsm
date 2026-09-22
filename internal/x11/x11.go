// Package x11 habla con el servidor X lo justo para saber si hay gestor de
// ventanas: conectarse, preguntar por un átomo y leer una propiedad.
//
// uxsm sólo usa la biblioteca estándar, así que no hay libX11 ni una llamada a
// xprop. Son tres peticiones del protocolo X11 y el saludo inicial; a cambio,
// uxsm no depende en tiempo de ejecución de ningún paquete de Xorg.
//
// El protocolo está en la especificación del X Window System, versión 11:
// el saludo en «Connection Setup», y las peticiones que se usan aquí,
// InternAtom y GetProperty, en «Requests».
package x11

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// le es el orden de bytes de toda la conversación. El cliente lo elige en el
// primer byte que envía y el servidor le responde en ese mismo orden, así que
// vale cualquiera; poco importa el de la máquina.
var le = binary.LittleEndian

// Window es el identificador de una ventana: un recurso del servidor X.
type Window uint32

// Átomos predefinidos que usa uxsm. Los predefinidos tienen número fijo y no
// hace falta preguntar por ellos con InternAtom.
const (
	atomString = 31 // STRING
	atomWindow = 33 // WINDOW
	atomWMName = 39 // WM_NAME
)

// Códigos de las peticiones que usa uxsm.
const (
	opInternAtom  = 16
	opGetProperty = 20
)

// replyTimeout es lo que se espera a cada respuesta del servidor. Sólo salta si
// el servidor X se queda colgado: las peticiones que hace uxsm se responden de
// inmediato.
const replyTimeout = 10 * time.Second

// Conn es una conexión abierta con el servidor X.
type Conn struct {
	nc   net.Conn
	root Window
	// seq es el número de la última petición enviada. El servidor numera las
	// peticiones desde 1 y devuelve ese número en cada respuesta y en cada
	// error, que es como se sabe a cuál corresponden.
	seq uint16
}

// Dial se conecta al servidor X de display, por ejemplo ":0" o "localhost:10.0",
// y hace el saludo inicial con la autorización que corresponda.
//
// Con display vacío se usa la variable DISPLAY.
func Dial(display string) (*Conn, error) {
	d, err := parseDisplay(display)
	if err != nil {
		return nil, err
	}
	nc, err := dial(d)
	if err != nil {
		return nil, fmt.Errorf("connecting to the X display %s: %w", d.display, err)
	}
	name, data, err := authFor(d)
	if err != nil {
		nc.Close()
		return nil, err
	}
	c := &Conn{nc: nc}
	if err := c.setup(d.screen, name, data); err != nil {
		nc.Close()
		return nil, fmt.Errorf("connecting to the X display %s: %w", d.display, err)
	}
	return c, nil
}

// Close cierra la conexión.
func (c *Conn) Close() error { return c.nc.Close() }

// Root es la ventana raíz de la pantalla del display: la que lleva las
// propiedades de EWMH que describen la sesión.
func (c *Conn) Root() Window { return c.root }

// setup envía el saludo del cliente y lee el del servidor, del que uxsm sólo
// necesita la ventana raíz de la pantalla screen.
func (c *Conn) setup(screen int, authName, authData []byte) error {
	req := make([]byte, 0, 12+len(authName)+pad(len(authName))+len(authData)+pad(len(authData)))
	req = append(req, 'l', 0)
	req = le.AppendUint16(req, 11) // versión mayor del protocolo
	req = le.AppendUint16(req, 0)  // versión menor
	req = le.AppendUint16(req, uint16(len(authName)))
	req = le.AppendUint16(req, uint16(len(authData)))
	req = le.AppendUint16(req, 0) // relleno
	req = appendPadded(req, authName)
	req = appendPadded(req, authData)

	if err := c.write(req); err != nil {
		return err
	}

	head := make([]byte, 8)
	if err := c.read(head); err != nil {
		return err
	}
	body := make([]byte, int(le.Uint16(head[6:8]))*4)
	if err := c.read(body); err != nil {
		return err
	}

	switch head[0] {
	case 1: // Success
		root, err := parseSetup(body, screen)
		if err != nil {
			return err
		}
		c.root = root
		return nil
	case 0: // Failed: el motivo son los primeros head[1] bytes del cuerpo
		reason := body
		if n := int(head[1]); n <= len(body) {
			reason = body[:n]
		}
		return fmt.Errorf("the X server refused the connection: %s", reason)
	default: // Authenticate, y cualquier cosa que no sea ninguno de los dos
		return errors.New("the X server asked for further authentication")
	}
}

// parseSetup saca del saludo del servidor la ventana raíz de la pantalla screen.
//
// Las pantallas van en una lista de longitud variable ―cada una lleva detrás sus
// profundidades y los visuales de cada profundidad―, así que hay que recorrerlas
// en orden aunque sólo interese una.
func parseSetup(body []byte, screen int) (Window, error) {
	if len(body) < 32 {
		return 0, errShortSetup
	}
	vendorLen := int(le.Uint16(body[16:18]))
	screens := int(body[20])
	formats := int(body[21])
	if screen >= screens {
		return 0, fmt.Errorf("the X server has no screen %d", screen)
	}

	// Detrás de la cabecera van el nombre del fabricante y los formatos de
	// imagen, que no se usan, y después las pantallas.
	off := 32 + vendorLen + pad(vendorLen) + formats*8
	for i := 0; i < screens; i++ {
		if off+40 > len(body) {
			return 0, errShortSetup
		}
		root := Window(le.Uint32(body[off : off+4]))
		if i == screen {
			return root, nil
		}
		depths := int(body[off+39])
		off += 40
		for d := 0; d < depths; d++ {
			if off+8 > len(body) {
				return 0, errShortSetup
			}
			off += 8 + int(le.Uint16(body[off+2:off+4]))*24
		}
	}
	return 0, errShortSetup
}

var errShortSetup = errors.New("the X server sent a malformed connection setup")

// Atom devuelve el número del átomo name. Si el átomo no existe todavía ―nadie
// ha usado esa propiedad en este servidor―, devuelve 0 sin crearlo.
func (c *Conn) Atom(name string) (uint32, error) {
	n := []byte(name)
	req := make([]byte, 0, 8+len(n)+pad(len(n)))
	req = append(req, opInternAtom, 1) // only-if-exists
	req = le.AppendUint16(req, uint16(2+(len(n)+pad(len(n)))/4))
	req = le.AppendUint16(req, uint16(len(n)))
	req = le.AppendUint16(req, 0) // relleno
	req = appendPadded(req, n)

	reply, _, err := c.roundTrip(req)
	if err != nil {
		return 0, fmt.Errorf("asking the X server for the atom %s: %w", name, err)
	}
	return le.Uint32(reply[8:12]), nil
}

// Property devuelve el valor de la propiedad property de la ventana w, si es del
// tipo typ. Si la ventana no tiene esa propiedad, o la tiene de otro tipo, el
// valor es nil.
//
// words es cuántas palabras de cuatro bytes se piden como mucho; con una
// propiedad más larga, el resto se descarta.
func (c *Conn) Property(w Window, property, typ uint32, words uint32) ([]byte, error) {
	req := make([]byte, 0, 24)
	req = append(req, opGetProperty, 0) // delete=0
	req = le.AppendUint16(req, 6)
	req = le.AppendUint32(req, uint32(w))
	req = le.AppendUint32(req, property)
	req = le.AppendUint32(req, typ)
	req = le.AppendUint32(req, 0) // desde el principio
	req = le.AppendUint32(req, words)

	reply, value, err := c.roundTrip(req)
	if err != nil {
		return nil, err
	}
	// format es 0, 8, 16 o 32 bits por unidad, y length, cuántas unidades hay.
	format := int(reply[1])
	length := int(le.Uint32(reply[16:20]))
	if format == 0 || length == 0 {
		return nil, nil
	}
	n := length * format / 8
	if n > len(value) {
		return nil, errors.New("the X server sent a malformed property")
	}
	return value[:n], nil
}

// roundTrip envía una petición y devuelve su respuesta: los 32 bytes fijos y,
// aparte, los datos variables que lleven detrás.
//
// El servidor puede mandar por el camino eventos y errores de otras peticiones;
// se descartan mirando el número de petición que lleva cada uno.
func (c *Conn) roundTrip(req []byte) (reply, extra []byte, err error) {
	if err := c.write(req); err != nil {
		return nil, nil, err
	}
	c.seq++
	want := c.seq

	for {
		head := make([]byte, 32)
		if err := c.read(head); err != nil {
			return nil, nil, err
		}
		seq := le.Uint16(head[2:4])

		switch {
		case head[0] == 0: // error
			if seq != want {
				continue
			}
			return nil, nil, &Error{Code: head[1], BadValue: le.Uint32(head[4:8])}
		case head[0] == 1: // respuesta
			extra := make([]byte, int(le.Uint32(head[4:8]))*4)
			if err := c.read(extra); err != nil {
				return nil, nil, err
			}
			if seq != want {
				continue
			}
			return head, extra, nil
		default: // evento
			// uxsm no pide eventos, pero los genéricos de una extensión
			// llevan datos detrás; si los hubiera, hay que saltarlos.
			if head[0]&0x7f == 35 {
				if err := c.read(make([]byte, int(le.Uint32(head[4:8]))*4)); err != nil {
					return nil, nil, err
				}
			}
		}
	}
}

func (c *Conn) write(b []byte) error {
	if err := c.nc.SetWriteDeadline(time.Now().Add(replyTimeout)); err != nil {
		return err
	}
	_, err := c.nc.Write(b)
	return err
}

func (c *Conn) read(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	if err := c.nc.SetReadDeadline(time.Now().Add(replyTimeout)); err != nil {
		return err
	}
	_, err := io.ReadFull(c.nc, b)
	return err
}

// Error es un error del protocolo: el servidor rechaza una petición, por
// ejemplo porque la ventana ya no existe.
type Error struct {
	Code     byte
	BadValue uint32
}

// Códigos de error del protocolo que uxsm distingue.
const (
	BadWindow = 3
)

func (e *Error) Error() string {
	name := map[byte]string{
		1: "BadRequest", 2: "BadValue", 3: "BadWindow", 4: "BadPixmap", 5: "BadAtom",
		6: "BadCursor", 7: "BadFont", 8: "BadMatch", 9: "BadDrawable", 10: "BadAccess",
	}[e.Code]
	if name == "" {
		name = fmt.Sprintf("error %d", e.Code)
	}
	return fmt.Sprintf("the X server answered %s (value %#x)", name, e.BadValue)
}

// isError dice si err es un error del protocolo con ese código.
func isError(err error, code byte) bool {
	var xerr *Error
	return errors.As(err, &xerr) && xerr.Code == code
}

// pad es el relleno que lleva detrás un bloque de n bytes para que la longitud
// sea múltiplo de cuatro, como pide el protocolo.
func pad(n int) int { return (4 - n%4) % 4 }

// appendPadded añade b y su relleno.
func appendPadded(dst, b []byte) []byte {
	dst = append(dst, b...)
	return append(dst, make([]byte, pad(len(b)))...)
}
