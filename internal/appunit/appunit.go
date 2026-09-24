// Package appunit arma la unidad de systemd con la que se lanza una aplicación
// dentro de una sesión gráfica, y la orden que la crea.
//
// Es lo que hace `uwsm app` en Wayland: cada aplicación en su propia unidad,
// dentro de uno de los slices de la sesión, en vez de todas juntas colgando del
// escritorio. Así se ven por separado, se les pueden poner límites, su registro
// va al diario con su nombre y se paran con la sesión.
package appunit

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// Los slices de la sesión, uno por clase de aplicación. El guion es jerarquía
// en systemd, así que cuelgan de los app.slice, background.slice y
// session.slice estándar. Los nombres llevan uxsm porque el paquete los
// instala, y dos paquetes no pueden traer el mismo fichero: los de uwsm, que
// gestiona así las sesiones de Wayland, se llaman *-graphical.slice.
const (
	AppSlice        = "app-uxsm.slice"
	BackgroundSlice = "background-uxsm.slice"
	SessionSlice    = "session-uxsm.slice"
)

// Slice traduce lo que se pide con -s al nombre del slice: las tres letras de
// uwsm, o un nombre entero para cualquier otro.
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

// Options es lo que hay que saber para lanzar una aplicación.
type Options struct {
	// Argv es la orden ya resuelta, con sus argumentos.
	Argv []string
	// Slice es el slice donde va, ya traducido por Slice.
	Slice string
	// Service la lanza como servicio en vez de como scope, que es lo de serie.
	// Un scope es la aplicación tal cual, lanzada por quien llama; un servicio
	// lo arranca el gestor, y sobrevive a quien lo pidió.
	Service bool
	// Entry es el ID de la entrada de la que sale, sin .desktop, si sale de una.
	Entry string
	// AppName sustituye al nombre que uxsm pondría en la unidad (-a), y
	// UnitName al nombre de unidad entero (-u).
	AppName, UnitName string
	// Description es la descripción de la unidad (-d).
	Description string
	// Silent es "out", "err" o "both": qué salida se tira. Sólo con servicio;
	// un scope hereda la del proceso que lo lanza, que es quien la puede
	// redirigir.
	Silent string
	// Properties son directivas de systemd para la unidad, "Clave=Valor", tal
	// como las toma systemd-run: TimeoutStopSec, MemoryMax, CPUQuota… Un scope
	// admite las de control de recursos y los plazos; las propias de un
	// servicio necesitan Service.
	Properties []string
	// WorkingDir es el Path= de la entrada, si lo trae.
	WorkingDir string
}

// suffix es el final del nombre de unidad de cada tipo.
func (o Options) suffix() string {
	if o.Service {
		return "service"
	}
	return "scope"
}

// Name es el nombre de la unidad: app-uxsm-<aplicación>-<azar>.scope, o con
// @<azar>.service si es un servicio.
//
// El formato es el que pide systemd para las aplicaciones: app-<quien la
// lanza>-<qué aplicación>-<algo que la distingue>. Lo del azar es porque la
// misma aplicación se puede lanzar varias veces, y cada vez necesita su unidad.
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

	// 255 es el máximo de systemd; lo que no es el nombre de la aplicación
	// ocupa "app-uxsm--12345678." más el sufijo.
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

// RunArgs devuelve la orden entera: systemd-run con sus opciones y, detrás, la
// aplicación.
//
// Un servicio se lanza con Type=exec y ExitType=cgroup: el gestor lo da por
// arrancado en cuanto ejecuta el programa, y lo da por terminado cuando no
// queda ningún proceso suyo, no cuando se va el primero.
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

// escape deja el nombre en algo que systemd admite en el nombre de una unidad:
// letras, números y ":", "_", "." y "-". Lo demás pasa a "_".
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

// randomHex son los ocho dígitos que distinguen una unidad de otra de la misma
// aplicación.
func randomHex() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
