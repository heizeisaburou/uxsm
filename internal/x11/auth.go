package x11

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// cookieName es el único método de autorización que usan los servidores X de un
// escritorio normal: un secreto que el display manager escribe en el fichero de
// autorización del usuario y el cliente le devuelve al conectarse.
const cookieName = "MIT-MAGIC-COOKIE-1"

// Familias de direcciones del fichero de autorización. Sólo se distinguen las
// que puede tener la entrada de un display local.
const (
	familyInternet = 0
	familyLocal    = 256
	familyWild     = 65535
)

// authEntry es una entrada del fichero de autorización: para qué display de qué
// máquina vale este secreto.
type authEntry struct {
	family  uint16
	address string
	number  string
	name    string
	data    []byte
}

// authFor busca el secreto con el que conectarse al display d.
//
// Si no hay fichero de autorización, o no hay entrada para este display, se
// devuelve una autorización vacía: el servidor decidirá si acepta la conexión
// ―lo hace, por ejemplo, un Xvfb arrancado sin -auth―.
func authFor(d display) (name, data []byte, err error) {
	path := os.Getenv("XAUTHORITY")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, nil
		}
		path = filepath.Join(home, ".Xauthority")
	}

	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading the X authority file: %w", err)
	}
	defer f.Close()

	entries, err := readAuth(f)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	e := matchAuth(entries, d)
	if e == nil {
		return nil, nil, nil
	}
	return []byte(e.name), e.data, nil
}

// readAuth lee las entradas de un fichero de autorización. Su formato son
// entradas seguidas, cada una con cinco campos por delante de los cuales va su
// longitud en dos bytes, y los números van siempre en orden de red.
func readAuth(r io.Reader) ([]authEntry, error) {
	var entries []authEntry
	for {
		var family uint16
		if err := binary.Read(r, binary.BigEndian, &family); err != nil {
			if errors.Is(err, io.EOF) {
				return entries, nil
			}
			return nil, err
		}
		fields := make([][]byte, 4)
		for i := range fields {
			var n uint16
			if err := binary.Read(r, binary.BigEndian, &n); err != nil {
				return nil, io.ErrUnexpectedEOF
			}
			fields[i] = make([]byte, n)
			if _, err := io.ReadFull(r, fields[i]); err != nil {
				return nil, io.ErrUnexpectedEOF
			}
		}
		entries = append(entries, authEntry{
			family:  family,
			address: string(fields[0]),
			number:  string(fields[1]),
			name:    string(fields[2]),
			data:    fields[3],
		})
	}
}

// matchAuth elige la entrada que vale para el display d, o nil si no hay
// ninguna.
//
// Tiene que valer el número del display y la máquina: una entrada sin número
// vale para cualquiera, y una de familia «wild», para cualquier máquina. Las
// entradas de un display local llevan el nombre de la máquina.
func matchAuth(entries []authEntry, d display) *authEntry {
	host, _ := os.Hostname()
	number := strconv.Itoa(d.number)

	for i := range entries {
		e := &entries[i]
		if e.name != cookieName {
			continue
		}
		if e.number != "" && e.number != number {
			continue
		}
		switch {
		case e.family == familyWild:
			return e
		case d.local() && e.family == familyLocal && e.address == host:
			return e
		case !d.local() && (e.family == familyLocal || e.family == familyInternet) && e.address == d.host:
			return e
		}
	}
	return nil
}
