package dm

import (
	"strings"
)

// setting es dónde está puesta una opción de un fichero INI, como las de
// LightDM y SDDM.
type setting struct {
	value string
	// file es el fichero que la pone; "" si ninguno la pone.
	file string
	// line es su número de línea en file, desde 0.
	line int
}

// lookup busca la opción key del grupo section en files, en orden, y devuelve
// la última que la pone: es la que manda, porque cada fichero pisa a los
// anteriores.
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

// findKey busca key en el grupo section de un texto INI y devuelve la línea de
// la última aparición y su valor.
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

// replaceLine devuelve text con la línea i cambiada por l.
func replaceLine(text string, i int, l string) string {
	lines := strings.Split(text, "\n")
	lines[i] = l
	return strings.Join(lines, "\n")
}

// splitList parte una lista de directorios con el separador sep, sin
// elementos vacíos y sin la barra final.
func splitList(s, sep string) []string {
	var dirs []string
	for _, d := range strings.Split(s, sep) {
		if d = strings.TrimSuffix(strings.TrimSpace(d), "/"); d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}
