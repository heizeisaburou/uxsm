package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/heizeisaburou/uxsm/internal/dm"
)

// checkResult es el resultado de una comprobación de uxsm check.
type checkResult struct {
	// status es "ok", "warning" o "unknown", cuando no se puede determinar.
	status string
	// summary es la frase con el resultado; details, lo que lo explica.
	summary string
	details []string
}

// checks son las comprobaciones de uxsm check, en el orden en que se enseñan.
// Cada una tiene el nombre con el que empieza su línea.
var checks = []struct {
	name string
	run  func() checkResult
}{
	{"xsessions dir", checkXSessionsDir},
}

// errWarnings es el error de uxsm check cuando alguna comprobación da aviso:
// sale con código 1, para poder usarlo en scripts.
var errWarnings = errors.New("some checks gave warnings")

// runCheck ejecuta todas las comprobaciones: `uxsm check`. Sin suborden las
// ejecuta todas, y cada línea dice qué ha comprobado y con qué resultado.
//
// La única suborden es `is-active`, que no es una comprobación del sistema sino
// una pregunta sobre la sesión de ahora mismo. Está aquí porque es donde la
// busca quien viene de uwsm, que la tiene igual.
func runCheck(args []string) error {
	if len(args) > 0 && args[0] == "is-active" {
		return runIsActive(args[1:])
	}

	fs := newFlagSet("check", "",
		"Run every uxsm check and show what each one checked and its result:\n"+
			"ok, warning, or unknown when it cannot be determined. Exits with 1 if\n"+
			"any check gives a warning.\n\n"+
			"Checks:\n"+
			"  xsessions dir  whether the display manager in use reads "+dm.LocalXSessions+",\n"+
			"                 where uxsm installs the session entries it generates\n\n"+
			"Subcommand:\n"+
			"  is-active      exit with 0 if a uxsm session is running, 1 if not")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return errUsage
	}

	warnings := false
	for _, c := range checks {
		r := c.run()
		fmt.Printf("%s: %s: %s\n", c.name, r.status, r.summary)
		for _, d := range r.details {
			fmt.Printf("  %s\n", d)
		}
		warnings = warnings || r.status == "warning"
	}
	if warnings {
		return errWarnings
	}
	return nil
}

// checkXSessionsDir comprueba si el display manager en uso lee
// dm.LocalXSessions: si no, las entradas que instala uxsm entry no salen en la
// pantalla de inicio.
func checkXSessionsDir() checkResult {
	r, err := dm.Active()
	if errors.Is(err, dm.ErrNoDisplayManager) {
		return checkResult{status: "unknown", summary: err.Error(),
			details: []string{"without a display manager there is no login screen that lists session entries"}}
	}
	if err != nil {
		return checkResult{status: "unknown", summary: err.Error()}
	}

	details := []string{
		"it reads: " + strings.Join(r.Dirs, ", "),
		"from: " + r.Origin,
	}
	if r.Reads(dm.LocalXSessions) {
		return checkResult{status: "ok", summary: r.Name + " (in use) reads " + dm.LocalXSessions, details: details}
	}
	details = append(details,
		"the session entries uxsm installs there will not show on the login screen",
		"to fix it: uxsm setup xsessions-dir")
	return checkResult{status: "warning", summary: r.Name + " (in use) does not read " + dm.LocalXSessions, details: details}
}
