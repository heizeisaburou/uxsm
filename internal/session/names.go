// Package session calcula la identidad de una sesión ―qué escritorio es y qué
// variables lo dicen― y guarda lo que el servicio de entorno necesita leer
// después.
package session

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrBadNames marca los errores de -D y -e: están mal los argumentos, no la
// sesión.
var ErrBadNames = errors.New("bad desktop names")

// namesPattern es el formato de -D: nombres de letras, números, "_", "." y "-",
// separados por ":". Es el mismo que acepta uwsm.
var namesPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(:[A-Za-z0-9_.-]+)*$`)

// ValidNames dice si s vale como -D: nombres separados por ":", cada uno de
// letras, números, "_", "." y "-".
func ValidNames(s string) bool {
	return namesPattern.MatchString(s)
}

// NamesOptions son las fuentes de los nombres del escritorio.
type NamesOptions struct {
	// Current es el XDG_CURRENT_DESKTOP que ya había en el entorno. Debería
	// haberlo puesto el display manager a partir del DesktopNames= de la entrada
	// que lanzó, pero no lo garantiza nadie: puede traer otra cosa o venir vacío.
	Current string
	// Entry son los DesktopNames= de la entrada de sesión.
	Entry []string
	// Flag es el valor de -D, separado por ":".
	Flag string
	// Exclusive es -e: sólo cuentan los nombres de -D.
	Exclusive bool
	// Executable es el nombre del programa, el del Exec= de la entrada o el del
	// comando: el último recurso.
	Executable string
}

// DesktopNames calcula los nombres que se usarán en XDG_CURRENT_DESKTOP,
// siguiendo el comportamiento de uwsm.
//
// Sin -e combina, en este orden:
//
//  1. XDG_CURRENT_DESKTOP ya presente en el entorno.
//  2. DesktopNames= de la entrada de sesión que ha leído uxsm.
//  3. Los nombres añadidos explícitamente con -D.
//
// Después elimina duplicados conservando la primera aparición. Si no queda
// ningún nombre, usa como último recurso el nombre del ejecutable.
//
// XDG_CURRENT_DESKTOP tiene prioridad porque representa la sesión que realmente
// lanzó el display manager. La especificación dice:
//
//	"XDG_CURRENT_DESKTOP should have been set by the login manager, according
//	to the value of the DesktopNames found in the session file."
//
// Esa frase es, además, prácticamente toda la definición que da el estándar
// sobre DesktopNames y los archivos de sesión: no especifica formalmente esos
// archivos ni obliga al display manager a hacer la conversión. Por eso no se
// puede asumir que XDG_CURRENT_DESKTOP coincida con el DesktopNames= de la
// entrada que uxsm haya encontrado por su cuenta.
//
// En condiciones normales ambas fuentes deberían describir lo mismo. Aun así,
// se conserva primero el valor del entorno porque procede de la entrada que el
// usuario eligió realmente en el display manager, mientras que uxsm podría
// acabar leyendo otra entrada.
//
// Los nombres de -D se añaden al final: amplían el resultado, no sustituyen lo
// anterior. Para sustituirlo está -e. Como los duplicados conservan su primera
// aparición, repetir un nombre no cambia la posición que ya tenía.
//
// Con -e se ignoran tanto XDG_CURRENT_DESKTOP como DesktopNames= y se usan
// exclusivamente los nombres dados con -D. Por eso -e sin -D es un error.
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

// splitNames parte una lista separada por ":" sin dejar elementos vacíos.
func splitNames(s string) []string {
	var names []string
	for _, n := range strings.Split(s, ":") {
		if n != "" {
			names = append(names, n)
		}
	}
	return names
}

// dedupe quita los repetidos y deja cada nombre donde apareció por primera vez.
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
