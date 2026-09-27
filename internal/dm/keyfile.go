package dm

import (
	"strings"
)

// setting records where an INI option, such as a LightDM or SDDM option, is set.
type setting struct {
	value string
	// file is the file that sets it, or "" if none does.
	file string
	// line is its zero-based line number in file.
	line int
}

// lookup searches files in order for option key in group section and returns
// the last setting, which takes precedence because each file overrides earlier
// ones.
func lookup(files []string, section, key string) (setting, error) {
	var found setting
	for _, f := range files {
		text, err := readFile(f)
		if err != nil {
			return setting{}, err
		}
		if i, v, ok := findKey(text, section, key); ok {
			found = setting{value: v, file: f, line: i}
		}
	}
	return found, nil
}

// findKey searches INI text for key in group section and returns the line and
// value of its last occurrence.
func findKey(text, section, key string) (int, string, bool) {
	line, value, ok := -1, "", false
	in := false
	for i, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "[") {
			in = l == "["+section+"]"
			continue
		}
		if !in || l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, ";") {
			continue
		}
		k, v, has := strings.Cut(l, "=")
		if has && strings.TrimSpace(k) == key {
			line, value, ok = i, strings.TrimSpace(v), true
		}
	}
	return line, value, ok
}

// replaceLine returns text with line i replaced by l.
func replaceLine(text string, i int, l string) string {
	lines := strings.Split(text, "\n")
	lines[i] = l
	return strings.Join(lines, "\n")
}

// splitList splits a directory list on sep, removing empty elements and
// trailing slashes.
func splitList(s, sep string) []string {
	var dirs []string
	for _, d := range strings.Split(s, sep) {
		if d = strings.TrimSuffix(strings.TrimSpace(d), "/"); d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}
