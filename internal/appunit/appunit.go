// Package appunit builds the systemd unit used to launch an application inside
// a graphical session, and the command that creates it.
//
// This is what `uwsm app` does on Wayland: each application gets its own unit
// in one of the session slices instead of all of them hanging off the desktop.
// This makes them visible separately, allows individual limits, records their
// logs under their names in the journal, and stops them with the session.
package appunit

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// Session slices, one per application class. A dash denotes hierarchy in
// systemd, so they are children of the standard app.slice, background.slice,
// and session.slice. Their names include uxsm because the package installs
// them and two packages cannot ship the same file; uwsm's equivalents for
// Wayland sessions are named *-graphical.slice.
const (
	AppSlice        = "app-uxsm.slice"
	BackgroundSlice = "background-uxsm.slice"
	SessionSlice    = "session-uxsm.slice"
)

// Slice translates the value passed to -s into a slice name: uwsm's three
// single-letter forms, or a full name for any other slice.
func Slice(s string) (string, error) {
	switch s {
	case "", "a":
		return AppSlice, nil
	case "b":
		return BackgroundSlice, nil
	case "s":
		return SessionSlice, nil
	}
	if !strings.HasSuffix(s, ".slice") {
		return "", fmt.Errorf("%q is not a slice: use a, b, s, or a name ending in .slice", s)
	}
	return s, nil
}

// Options contains everything needed to launch an application.
type Options struct {
	// Argv is the resolved command and its arguments.
	Argv []string
	// Slice is the destination slice, already translated by Slice.
	Slice string
	// Service launches it as a service instead of the default scope. A scope is
	// the application itself, launched by the caller; a service is started by
	// the manager and outlives the process that requested it.
	Service bool
	// Entry is the ID, without .desktop, of the entry it came from, if any.
	Entry string
	// AppName replaces the name uxsm would put in the unit (-a), while UnitName
	// replaces the entire unit name (-u).
	AppName, UnitName string
	// Description is the unit description (-d).
	Description string
	// Silent is "out", "err", or "both": which output to discard. It only
	// applies to services; a scope inherits output from the launching process,
	// which is responsible for redirecting it.
	Silent string
	// Properties are systemd unit directives in "Key=Value" form, as accepted
	// by systemd-run: TimeoutStopSec, MemoryMax, CPUQuota… A scope accepts
	// resource-control and timeout properties; service-specific properties
	// require Service.
	Properties []string
	// WorkingDir is the entry's Path=, if present.
	WorkingDir string
}

// suffix is the ending of each unit type's name.
func (o Options) suffix() string {
	if o.Service {
		return "service"
	}
	return "scope"
}

// Name is the unit name: app-uxsm-<application>-<random>.scope, or with
// @<random>.service for a service.
//
// This is systemd's application naming format:
// app-<launcher>-<application>-<unique identifier>. The random component is
// needed because the same application may be launched more than once and each
// launch needs its own unit.
func (o Options) Name() (string, error) {
	if o.UnitName != "" {
		if !strings.HasSuffix(o.UnitName, "."+o.suffix()) {
			return "", fmt.Errorf("the unit name %q does not end in .%s", o.UnitName, o.suffix())
		}
		if len(o.UnitName) > 255 {
			return "", fmt.Errorf("the unit name is too long (%d > 255)", len(o.UnitName))
		}
		return o.UnitName, nil
	}

	name := o.AppName
	if name == "" {
		name = o.Entry
	}
	if name == "" && len(o.Argv) > 0 {
		name = filepath.Base(o.Argv[0])
	}
	name = escape(name)

	// systemd's limit is 255; everything except the application name occupies
	// "app-uxsm--12345678." plus the suffix.
	room := 255 - len("app-uxsm--12345678.") - len(o.suffix())
	if len(name) > room {
		name = name[:room]
	}

	random, err := randomHex()
	if err != nil {
		return "", err
	}
	if o.Service {
		return fmt.Sprintf("app-uxsm-%s@%s.service", name, random), nil
	}
	return fmt.Sprintf("app-uxsm-%s-%s.scope", name, random), nil
}

// RunArgs returns the complete command: systemd-run with its options followed
// by the application.
//
// A service uses Type=exec and ExitType=cgroup: the manager considers it
// started once it executes the program, and finished when none of its processes
// remain, rather than when the first process exits.
func (o Options) RunArgs() ([]string, error) {
	if len(o.Argv) == 0 {
		return nil, fmt.Errorf("no command to run")
	}
	name, err := o.Name()
	if err != nil {
		return nil, err
	}

	args := []string{"systemd-run", "--user", "--unit=" + name, "--slice=" + o.Slice}
	if o.Service {
		args = append(args, "--property=Type=exec", "--property=ExitType=cgroup")
	} else {
		args = append(args, "--scope")
	}
	if o.Description != "" {
		args = append(args, "--description="+o.Description)
	}
	if o.WorkingDir != "" {
		args = append(args, "--working-directory="+o.WorkingDir)
	}
	for _, p := range o.Properties {
		key, _, ok := strings.Cut(p, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("%q is not a unit property: they go as Key=Value", p)
		}
		args = append(args, "--property="+p)
	}
	switch o.Silent {
	case "":
	case "out":
		args = append(args, "--property=StandardOutput=null")
	case "err":
		args = append(args, "--property=StandardError=null")
	case "both":
		args = append(args, "--property=StandardOutput=null", "--property=StandardError=null")
	default:
		return nil, fmt.Errorf("%q is not what to silence: use out, err or both", o.Silent)
	}

	return append(append(args, "--"), o.Argv...), nil
}

// escape turns the name into something systemd accepts in a unit name: letters,
// digits, and ":", "_", ".", and "-". Everything else becomes "_".
func escape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == ':', c == '_', c == '.', c == '-':
			b.WriteByte(c)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// randomHex returns the eight digits that distinguish units for separate
// launches of the same application.
func randomHex() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
