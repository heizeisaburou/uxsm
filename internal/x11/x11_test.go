package x11

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseDisplay(t *testing.T) {
	t.Setenv("DISPLAY", ":7")

	cases := []struct {
		in     string
		host   string
		number int
		screen int
		bad    bool
	}{
		{in: ":0"},
		{in: ":5.1", number: 5, screen: 1},
		{in: "unix:2", host: "unix", number: 2},
		{in: "localhost:10.0", host: "localhost", number: 10},
		{in: "", number: 7}, // de DISPLAY
		{in: "bspwm", bad: true},
		{in: ":", bad: true},
		{in: ":x", bad: true},
		{in: ":0.x", bad: true},
	}
	for _, c := range cases {
		d, err := parseDisplay(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("parseDisplay(%q) did not fail", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDisplay(%q): %v", c.in, err)
			continue
		}
		if d.host != c.host || d.number != c.number || d.screen != c.screen {
			t.Errorf("parseDisplay(%q) = %+v, want host %q, display %d, screen %d",
				c.in, d, c.host, c.number, c.screen)
		}
	}
}

func TestReadAuthAndMatch(t *testing.T) {
	file := bytes.Join([][]byte{
		authRecord(familyLocal, "otherhost", "0", cookieName, []byte("wronghost")),
		authRecord(familyLocal, "thishost", "1", "XDM-AUTHORIZATION-1", []byte("wrongmethod")),
		authRecord(familyLocal, "thishost", "1", cookieName, []byte("good")),
		authRecord(familyWild, "", "", cookieName, []byte("wild")),
	}, nil)

	entries, err := readAuth(bytes.NewReader(file))
	if err != nil {
		t.Fatalf("readAuth: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("readAuth returned %d entries, want 4", len(entries))
	}

	// matchAuth compara con el nombre de esta máquina, así que la entrada que
	// tiene que ganar lleva el suyo.
	host := entries[2].address
	if h, err := os.Hostname(); err == nil {
		host = h
	}
	for i := range entries {
		if entries[i].address == "thishost" {
			entries[i].address = host
		}
	}

	got := matchAuth(entries, display{number: 1})
	if got == nil || string(got.data) != "good" {
		t.Errorf("matchAuth chose %+v, want the local entry of display 1", got)
	}
	// Para otro display sólo vale la entrada que sirve para cualquiera.
	got = matchAuth(entries, display{number: 9})
	if got == nil || string(got.data) != "wild" {
		t.Errorf("matchAuth for display 9 chose %+v, want the wild entry", got)
	}
}

func TestReadAuthTruncated(t *testing.T) {
	file := authRecord(familyLocal, "thishost", "0", cookieName, []byte("good"))
	if _, err := readAuth(bytes.NewReader(file[:len(file)-2])); err == nil {
		t.Error("readAuth accepted a truncated file")
	}
}

// TestManager recorre la comprobación de EWMH entera contra un servidor X de
// mentira: sin propiedad no hay gestor de ventanas, con una marca que no se
// confirma tampoco, y cuando se confirma sale su nombre.
func TestManager(t *testing.T) {
	const root, wm = 0x111, 0x222

	srv := &fakeX{
		root:  root,
		atoms: map[string]uint32{"_NET_SUPPORTING_WM_CHECK": 300, "UTF8_STRING": 301, "_NET_WM_NAME": 302},
		props: map[propKey][]byte{},
	}
	c := srv.start(t)

	if got, err := c.Manager(); err != nil || got != nil {
		t.Fatalf("Manager without the property = %v, %v; want nil, nil", got, err)
	}

	// La raíz apunta a una ventana que no existe: es la marca de un gestor de
	// ventanas que ya no está.
	srv.props[propKey{root, 300}] = window(wm)
	if got, err := c.Manager(); err != nil || got != nil {
		t.Fatalf("Manager with a stale mark = %v, %v; want nil, nil", got, err)
	}

	// La ventana existe pero apunta a otra: tampoco vale.
	srv.windows = map[uint32]bool{wm: true}
	srv.props[propKey{wm, 300}] = window(root)
	if got, err := c.Manager(); err != nil || got != nil {
		t.Fatalf("Manager with a mark pointing elsewhere = %v, %v; want nil, nil", got, err)
	}

	srv.props[propKey{wm, 300}] = window(wm)
	srv.props[propKey{wm, 302}] = []byte("bspwm")
	got, err := c.Manager()
	if err != nil {
		t.Fatalf("Manager: %v", err)
	}
	if got == nil || got.Window != wm || got.Name != "bspwm" {
		t.Fatalf("Manager = %+v, want window %#x named bspwm", got, wm)
	}
	if got.String() != "bspwm" {
		t.Errorf("String() = %q, want bspwm", got.String())
	}
}

// TestManagerSkipsEvents comprueba que un evento que llegue por el camino no se
// toma por la respuesta de la petición.
func TestManagerSkipsEvents(t *testing.T) {
	srv := &fakeX{
		root:    0x111,
		atoms:   map[string]uint32{"_NET_SUPPORTING_WM_CHECK": 300},
		props:   map[propKey][]byte{},
		events:  1,
		windows: map[uint32]bool{},
	}
	c := srv.start(t)
	if got, err := c.Manager(); err != nil || got != nil {
		t.Fatalf("Manager = %v, %v; want nil, nil", got, err)
	}
}

func TestWaitForManagerTimeout(t *testing.T) {
	srv := &fakeX{root: 0x111, atoms: map[string]uint32{}, props: map[propKey][]byte{}}
	c := srv.start(t)

	start := time.Now()
	_, err := waitForManager(c, 150*time.Millisecond, 10*time.Millisecond)
	if err == nil {
		t.Fatal("waitForManager did not fail without a window manager")
	}
	if !strings.Contains(err.Error(), "no EWMH window manager") {
		t.Errorf("waitForManager: %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("waitForManager waited %s, much longer than its timeout", d)
	}
}

// authRecord arma una entrada de fichero de autorización.
func authRecord(family uint16, address, number, name string, data []byte) []byte {
	var b []byte
	b = binary.BigEndian.AppendUint16(b, family)
	for _, f := range [][]byte{[]byte(address), []byte(number), []byte(name), data} {
		b = binary.BigEndian.AppendUint16(b, uint16(len(f)))
		b = append(b, f...)
	}
	return b
}

func window(id uint32) []byte { return le.AppendUint32(nil, id) }

type propKey struct {
	window   uint32
	property uint32
}

// fakeX es un servidor X de mentira: contesta al saludo y a las dos peticiones
// que usa uxsm, InternAtom y GetProperty, con lo que le hayan puesto.
type fakeX struct {
	root    uint32
	atoms   map[string]uint32
	props   map[propKey][]byte
	windows map[uint32]bool
	// events es cuántos eventos mete por delante de la primera respuesta.
	events int
}

// start arranca el servidor y devuelve la conexión del cliente, ya saludada.
func (s *fakeX) start(t *testing.T) *Conn {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { client.Close() })
	go s.serve(server)

	c := &Conn{nc: client}
	if err := c.setup(0, nil, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if c.Root() != Window(s.root) {
		t.Fatalf("root window = %#x, want %#x", c.Root(), s.root)
	}
	return c
}

func (s *fakeX) serve(nc net.Conn) {
	defer nc.Close()

	head := make([]byte, 12)
	if _, err := io.ReadFull(nc, head); err != nil {
		return
	}
	// Nombre y datos de autorización, cada uno con su relleno.
	for _, n := range []int{int(le.Uint16(head[6:8])), int(le.Uint16(head[8:10]))} {
		if _, err := io.ReadFull(nc, make([]byte, n+pad(n))); err != nil {
			return
		}
	}
	if _, err := nc.Write(s.setupReply()); err != nil {
		return
	}

	var seq uint16
	for {
		req := make([]byte, 4)
		if _, err := io.ReadFull(nc, req); err != nil {
			return
		}
		rest := make([]byte, (int(le.Uint16(req[2:4]))-1)*4)
		if _, err := io.ReadFull(nc, rest); err != nil {
			return
		}
		seq++

		for ; s.events > 0; s.events-- {
			event := make([]byte, 32)
			event[0] = 2 // KeyPress
			if _, err := nc.Write(event); err != nil {
				return
			}
		}

		var reply []byte
		switch req[0] {
		case opInternAtom:
			name := string(rest[4 : 4+int(le.Uint16(rest[:2]))])
			reply = make([]byte, 32)
			reply[0] = 1
			le.PutUint16(reply[2:4], seq)
			le.PutUint32(reply[8:12], s.atoms[name])
		case opGetProperty:
			win := le.Uint32(rest[:4])
			if win != s.root && !s.windows[win] {
				reply = make([]byte, 32)
				le.PutUint16(reply[2:4], seq)
				reply[1] = BadWindow
				le.PutUint32(reply[4:8], win)
				break
			}
			value := s.props[propKey{win, le.Uint32(rest[4:8])}]
			reply = make([]byte, 32, 32+len(value)+pad(len(value)))
			reply[0] = 1
			reply[1] = 8 // bits por unidad
			le.PutUint16(reply[2:4], seq)
			le.PutUint32(reply[4:8], uint32((len(value)+pad(len(value)))/4))
			le.PutUint32(reply[16:20], uint32(len(value)))
			reply = appendPadded(reply, value)
		default:
			return
		}
		if _, err := nc.Write(reply); err != nil {
			return
		}
	}
}

// setupReply arma un saludo de servidor con dos pantallas, para que el cliente
// tenga que recorrer la primera con sus profundidades para llegar a la segunda.
func (s *fakeX) setupReply() []byte {
	vendor := []byte("uxsm fake")
	body := make([]byte, 32)
	le.PutUint16(body[16:18], uint16(len(vendor)))
	body[20] = 2 // pantallas
	body[21] = 1 // formatos de imagen
	body = appendPadded(body, vendor)
	body = append(body, make([]byte, 8)...) // el formato de imagen

	for i, root := range []uint32{s.root, s.root + 1} {
		screen := make([]byte, 40)
		le.PutUint32(screen[:4], root)
		screen[39] = byte(i) // profundidades, una en la primera pantalla
		body = append(body, screen...)
		if i == 0 {
			depth := make([]byte, 8)
			le.PutUint16(depth[2:4], 1) // un visual
			body = append(body, depth...)
			body = append(body, make([]byte, 24)...)
		}
	}

	reply := make([]byte, 8)
	reply[0] = 1 // Success
	le.PutUint16(reply[2:4], 11)
	le.PutUint16(reply[6:8], uint16(len(body)/4))
	return append(reply, body...)
}
