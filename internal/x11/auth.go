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

// cookieName is the only authorization method used by X servers for a normal
// desktop: a secret the display manager writes to the user's authorization file
// and the client sends back when connecting.
const cookieName = "MIT-MAGIC-COOKIE-1"

// Address families in the authorization file. Only those that may occur in a
// local display entry are distinguished.
const (
	familyInternet = 0
	familyLocal    = 256
	familyWild     = 65535
)

// authEntry is an authorization-file entry: the machine and display for which
// this secret is valid.
type authEntry struct {
	family  uint16
	address string
	number  string
	name    string
	data    []byte
}

// authFor finds the secret used to connect to display d.
//
// If there is no authorization file or no entry for this display, it returns
// empty authorization and lets the server decide whether to accept the
// connection, as an Xvfb started without -auth does.
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

// readAuth reads entries from an authorization file. It consists of consecutive
// entries, each with five fields prefixed by a two-byte length; numbers always
// use network byte order.
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

// matchAuth selects the entry valid for display d, or nil if none applies.
//
// Both display number and machine must match: an entry without a number applies
// to any display, and a "wild" family entry applies to any machine. Local
// display entries contain the machine name.
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
