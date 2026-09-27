// Package x11 communicates with the X server just enough to determine whether
// a window manager is present: connect, query an atom, and read a property.
//
// uxsm uses only the standard library, with neither libX11 nor an xprop call.
// It performs three X11 protocol requests and the initial handshake; in return,
// uxsm has no runtime dependency on an Xorg package.
//
// The protocol is defined by the X Window System Protocol, Version 11: the
// handshake under "Connection Setup", and the requests used here, InternAtom
// and GetProperty, under "Requests".
package x11

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// le is the byte order for the entire conversation. The client selects it in
// the first byte it sends and the server replies in the same order, so either
// order works regardless of the machine's native byte order.
var le = binary.LittleEndian

// Window is a window identifier, an X server resource.
type Window uint32

// Predefined atoms used by uxsm. Their numbers are fixed, so InternAtom is not
// needed for them.
const (
	atomString = 31 // STRING
	atomWindow = 33 // WINDOW
	atomWMName = 39 // WM_NAME
)

// Request opcodes used by uxsm.
const (
	opInternAtom  = 16
	opGetProperty = 20
)

// replyTimeout limits the wait for each server reply. It only expires if the X
// server hangs; the requests uxsm makes normally receive immediate responses.
const replyTimeout = 10 * time.Second

// Conn is an open connection to the X server.
type Conn struct {
	nc   net.Conn
	root Window
	// seq is the number of the last request sent. The server numbers requests
	// from 1 and returns that number in every reply and error, allowing each one
	// to be matched to its request.
	seq uint16
}

// Dial connects to the X server at display, such as ":0" or "localhost:10.0",
// and performs the initial handshake with the appropriate authorization.
//
// If display is empty, the DISPLAY variable is used.
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

// Close closes the connection.
func (c *Conn) Close() error { return c.nc.Close() }

// Root is the root window for the display's screen, which carries the EWMH
// properties describing the session.
func (c *Conn) Root() Window { return c.root }

// setup sends the client handshake and reads the server response, from which
// uxsm only needs the root window of screen.
func (c *Conn) setup(screen int, authName, authData []byte) error {
	req := make([]byte, 0, 12+len(authName)+pad(len(authName))+len(authData)+pad(len(authData)))
	req = append(req, 'l', 0)
	req = le.AppendUint16(req, 11) // protocol major version
	req = le.AppendUint16(req, 0)  // minor version
	req = le.AppendUint16(req, uint16(len(authName)))
	req = le.AppendUint16(req, uint16(len(authData)))
	req = le.AppendUint16(req, 0) // padding
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
	case 0: // Failed: the reason is in the first head[1] bytes of the body
		reason := body
		if n := int(head[1]); n <= len(body) {
			reason = body[:n]
		}
		return fmt.Errorf("the X server refused the connection: %s", reason)
	default: // Authenticate, and anything other than the two cases above
		return errors.New("the X server asked for further authentication")
	}
}

// parseSetup extracts the root window of screen from the server handshake.
//
// Screens form a variable-length list—each followed by its depths and each
// depth's visuals—so they must be traversed in order even when only one matters.
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

	// The header is followed by the unused vendor name and pixmap formats, then
	// by the screens.
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

// Atom returns the number of atom name. If the atom does not exist yet because
// nobody has used that property on this server, it returns 0 without creating it.
func (c *Conn) Atom(name string) (uint32, error) {
	n := []byte(name)
	req := make([]byte, 0, 8+len(n)+pad(len(n)))
	req = append(req, opInternAtom, 1) // only-if-exists
	req = le.AppendUint16(req, uint16(2+(len(n)+pad(len(n)))/4))
	req = le.AppendUint16(req, uint16(len(n)))
	req = le.AppendUint16(req, 0) // padding
	req = appendPadded(req, n)

	reply, _, err := c.roundTrip(req)
	if err != nil {
		return 0, fmt.Errorf("asking the X server for the atom %s: %w", name, err)
	}
	return le.Uint32(reply[8:12]), nil
}

// Property returns property from window w if it has type typ. If the window has
// no such property or the property has another type, the value is nil.
//
// words is the maximum number of four-byte words requested; any remaining data
// in a longer property is discarded.
func (c *Conn) Property(w Window, property, typ uint32, words uint32) ([]byte, error) {
	req := make([]byte, 0, 24)
	req = append(req, opGetProperty, 0) // delete=0
	req = le.AppendUint16(req, 6)
	req = le.AppendUint32(req, uint32(w))
	req = le.AppendUint32(req, property)
	req = le.AppendUint32(req, typ)
	req = le.AppendUint32(req, 0) // from the beginning
	req = le.AppendUint32(req, words)

	reply, value, err := c.roundTrip(req)
	if err != nil {
		return nil, err
	}
	// format is 0, 8, 16, or 32 bits per unit; length is the number of units.
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

// roundTrip sends a request and returns its reply: the fixed 32 bytes and any
// variable data that follows.
//
// The server may interleave events and errors from other requests; they are
// discarded based on the request number each carries.
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
		default: // event
			// uxsm does not request events, but generic extension events carry
			// trailing data that must be skipped if present.
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

// Error is a protocol error: the server rejects a request, for example because
// the window no longer exists.
type Error struct {
	Code     byte
	BadValue uint32
}

// Protocol error codes distinguished by uxsm.
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

// isError says whether err is a protocol error with the given code.
func isError(err error, code byte) bool {
	var xerr *Error
	return errors.As(err, &xerr) && xerr.Code == code
}

// pad is the trailing padding needed to make an n-byte block's length a multiple
// of four, as required by the protocol.
func pad(n int) int { return (4 - n%4) % 4 }

// appendPadded appends b and its padding.
func appendPadded(dst, b []byte) []byte {
	dst = append(dst, b...)
	return append(dst, make([]byte, pad(len(b)))...)
}
