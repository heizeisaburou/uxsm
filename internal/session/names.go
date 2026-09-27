// Package session computes a session's identity—which desktop it is and which
// variables describe it—and stores what the environment service must read later.
package session

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrBadNames marks -D and -e errors: the arguments, not the session, are invalid.
var ErrBadNames = errors.New("bad desktop names")

// namesPattern is the -D format: names made of letters, digits, "_", ".", and
// "-", separated by ":". It is the same format accepted by uwsm.
var namesPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(:[A-Za-z0-9_.-]+)*$`)

// ValidNames says whether s is valid for -D: colon-separated names made of
// letters, digits, "_", ".", and "-".
func ValidNames(s string) bool {
	return namesPattern.MatchString(s)
}

// NamesOptions contains the sources of desktop names.
type NamesOptions struct {
	// Current is the XDG_CURRENT_DESKTOP already present in the environment. The
	// display manager should have derived it from DesktopNames= in the entry it
	// launched, but this is not guaranteed: it may contain something else or be
	// empty.
	Current string
	// Entry contains DesktopNames= from the session entry.
	Entry []string
	// Flag is the colon-separated -D value.
	Flag string
	// Exclusive is -e: only names from -D count.
	Exclusive bool
	// Executable is the program name from the entry's Exec= or the command: the
	// final fallback.
	Executable string
}

// DesktopNames computes the names used in XDG_CURRENT_DESKTOP, following uwsm's
// behavior.
//
// Without -e, it combines these sources in order:
//
//  1. XDG_CURRENT_DESKTOP already present in the environment.
//  2. DesktopNames= from the session entry read by uxsm.
//  3. Names explicitly added with -D.
//
// It then removes duplicates while preserving the first occurrence. If no name
// remains, it falls back to the executable name.
//
// XDG_CURRENT_DESKTOP has priority because it represents the session the
// display manager actually launched. The specification says:
//
//	"XDG_CURRENT_DESKTOP should have been set by the login manager, according
//	to the value of the DesktopNames found in the session file."
//
// That sentence is also nearly the standard's entire definition of
// DesktopNames and session files: it does not formally specify those files or
// require the display manager to perform the conversion. Therefore
// XDG_CURRENT_DESKTOP cannot be assumed to match DesktopNames= in the entry
// found independently by uxsm.
//
// Under normal conditions both sources should describe the same desktop. The
// environment value still comes first because it originates from the entry the
// user actually chose in the display manager, whereas uxsm might end up reading
// a different entry.
//
// Names from -D are appended: they extend the result rather than replace earlier
// sources. Replacement is the purpose of -e. Because duplicate removal keeps
// the first occurrence, repeating a name does not change its position.
//
// With -e, both XDG_CURRENT_DESKTOP and DesktopNames= are ignored and only names
// passed with -D are used. This is why -e without -D is an error.
func DesktopNames(o NamesOptions) ([]string, error) {
	if o.Flag != "" && !namesPattern.MatchString(o.Flag) {
		return nil, fmt.Errorf("%w: %q: use letters, digits, '_', '.' and '-', separated by ':'", ErrBadNames, o.Flag)
	}

	if o.Exclusive {
		if o.Flag == "" {
			return nil, fmt.Errorf("%w: -e needs desktop names given with -D", ErrBadNames)
		}
		return strings.Split(o.Flag, ":"), nil
	}

	var names []string
	names = append(names, splitNames(o.Current)...)
	names = append(names, o.Entry...)
	names = append(names, splitNames(o.Flag)...)
	names = dedupe(names)

	if len(names) == 0 && o.Executable != "" {
		names = []string{o.Executable}
	}
	if len(names) == 0 {
		return nil, errors.New("no desktop names: the entry has no DesktopNames= and nothing else gives one")
	}
	return names, nil
}

// splitNames splits a colon-separated list without retaining empty elements.
func splitNames(s string) []string {
	var names []string
	for _, n := range strings.Split(s, ":") {
		if n != "" {
			names = append(names, n)
		}
	}
	return names
}

// dedupe removes duplicates and keeps each name at its first occurrence.
func dedupe(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := names[:0]
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}
