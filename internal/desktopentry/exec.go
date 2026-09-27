package desktopentry

import (
	"errors"
	"fmt"
	"strings"
)

// Fields contains the values substituted for field codes in Exec= when an
// application is launched. If every field is empty, the codes disappear, which
// is appropriate for a session because it receives no files or icons.
type Fields struct {
	// Args contains files or URLs passed to the application: %f and %u use the
	// first, while %F and %U use all of them.
	Args []string
	// Icon is the entry's Icon=. %i becomes two arguments, "--icon" and the
	// icon; without an icon, it becomes no arguments.
	Icon string
	// Name is the entry's Name=, which supplies %c.
	Name string
	// Path is the entry file's path, which supplies %k.
	Path string
}

// SplitExec converts an Exec= value into the argument list to execute without
// substituting field codes.
func SplitExec(exec string) ([]string, error) {
	return SplitExecFields(exec, Fields{})
}

// SplitExecFields is SplitExec with field codes substituted from f according
// to the Desktop Entry Specification:
//
//   - General string escapes (\s, \\…) are decoded first.
//   - Arguments are separated by spaces.
//   - A double-quoted argument may contain spaces; within it, double quotes,
//     backticks, dollar signs, and backslashes are escaped with a backslash.
//   - %% becomes a literal %.
//   - Field codes deprecated by the specification (%d, %D, %n, %N, %v, %m)
//     are removed as required by the specification itself.
//
// Codes that may produce multiple arguments—%f, %F, %u, %U, and %i—produce
// separate arguments instead of attaching them to adjacent text, because the
// specification requires them to occupy an argument by themselves.
func SplitExecFields(exec string, f Fields) ([]string, error) {
	s := unescape(exec)

	var args []string
	var cur strings.Builder
	inArg, inQuotes := false, false

	// flush finishes the argument currently being built, if any.
	flush := func() {
		if inArg {
			args = append(args, cur.String())
			cur.Reset()
			inArg = false
		}
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuotes && c == '\\' && i+1 < len(s) && strings.IndexByte("\"`$\\", s[i+1]) >= 0:
			cur.WriteByte(s[i+1])
			i++
		case c == '"':
			inQuotes = !inQuotes
			inArg = true
		case !inQuotes && c == ' ':
			flush()
		case !inQuotes && c == '%' && i+1 < len(s):
			i++
			switch s[i] {
			case '%':
				cur.WriteByte('%')
				inArg = true
			case 'f', 'u':
				flush()
				if len(f.Args) > 0 {
					args = append(args, f.Args[0])
				}
			case 'F', 'U':
				flush()
				args = append(args, f.Args...)
			case 'i':
				flush()
				if f.Icon != "" {
					args = append(args, "--icon", f.Icon)
				}
			case 'c':
				cur.WriteString(f.Name)
				inArg = true
			case 'k':
				cur.WriteString(f.Path)
				inArg = true
			case 'd', 'D', 'n', 'N', 'v', 'm':
				// deprecated: the specification requires them to be removed
			default:
				return nil, fmt.Errorf("invalid field code %%%c in Exec=%q", s[i], exec)
			}
		default:
			cur.WriteByte(c)
			inArg = true
		}
	}
	if inQuotes {
		return nil, fmt.Errorf("unterminated quote in Exec=%q", exec)
	}
	flush()
	if len(args) == 0 {
		return nil, errors.New("empty Exec=")
	}

	return args, nil
}
