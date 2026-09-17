package desktopentry

import (
	"errors"
	"fmt"
	"strings"
)

// SplitExec convierte el valor de Exec= en la lista de argumentos que se
// ejecutaría, siguiendo la Desktop Entry Specification:
//
//   - Primero se deshacen los escapes generales de las cadenas (\s, \\…).
//   - Los argumentos se separan por espacios.
//   - Un argumento entre comillas dobles puede llevar espacios; dentro, la
//     comilla doble, la comilla invertida, el dólar y la barra invertida van
//     escapados con una barra invertida.
//   - Los códigos de campo (%f, %u, %i…) se quitan, porque una sesión no recibe
//     ficheros ni URLs, y %% queda como un % literal.
func SplitExec(exec string) ([]string, error) {
	s := unescape(exec)

	var args []string
	var cur strings.Builder
	inArg, inQuotes := false, false

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
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		case !inQuotes && c == '%' && i+1 < len(s):
			i++
			switch s[i] {
			case '%':
				cur.WriteByte('%')
				inArg = true
			case 'f', 'F', 'u', 'U', 'i', 'c', 'k', 'd', 'D', 'n', 'N', 'v', 'm':
				// código de campo: no se sustituye por nada
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
	if inArg {
		args = append(args, cur.String())
	}
	if len(args) == 0 {
		return nil, errors.New("empty Exec=")
	}

	return args, nil
}
