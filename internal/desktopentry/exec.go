package desktopentry

import (
	"errors"
	"fmt"
	"strings"
)

// Fields son los valores con los que se sustituyen los códigos de campo de un
// Exec= al lanzar una aplicación. Con todo vacío, los códigos desaparecen, que
// es lo que quiere una sesión: no recibe ficheros ni iconos.
type Fields struct {
	// Args son los ficheros o URLs que se le pasan a la aplicación: %f y %u
	// toman el primero, %F y %U los toman todos.
	Args []string
	// Icon es el Icon= de la entrada. %i se convierte en dos argumentos,
	// "--icon" y el icono; sin icono, en ninguno.
	Icon string
	// Name es el Name= de la entrada, que es lo que pone %c.
	Name string
	// Path es la ruta del fichero de la entrada, que es lo que pone %k.
	Path string
}

// SplitExec convierte el valor de Exec= en la lista de argumentos que se
// ejecutaría, sin sustituir los códigos de campo.
func SplitExec(exec string) ([]string, error) {
	return SplitExecFields(exec, Fields{})
}

// SplitExecFields es SplitExec sustituyendo los códigos de campo por f,
// siguiendo la Desktop Entry Specification:
//
//   - Primero se deshacen los escapes generales de las cadenas (\s, \\…).
//   - Los argumentos se separan por espacios.
//   - Un argumento entre comillas dobles puede llevar espacios; dentro, la
//     comilla doble, la comilla invertida, el dólar y la barra invertida van
//     escapados con una barra invertida.
//   - %% queda como un % literal.
//   - Los códigos que la especificación da por obsoletos (%d, %D, %n, %N, %v,
//     %m) se quitan, como pide ella misma.
//
// Los códigos que pueden dar varios argumentos ―%f, %F, %u, %U, %i― los dan
// sueltos, sin pegarse a lo que tengan al lado: la especificación pide que
// vayan solos en su argumento.
func SplitExecFields(exec string, f Fields) ([]string, error) {
	s := unescape(exec)

	var args []string
	var cur strings.Builder
	inArg, inQuotes := false, false

	// flush cierra el argumento que se esté construyendo, si hay alguno.
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
				// obsoletos: la especificación dice que se quiten
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
