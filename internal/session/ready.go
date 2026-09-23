package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadyFile es la señal de que la sesión está lista: hay escritorio en
// pantalla, y lo que arranque detrás tiene dónde colocarse.
//
// Se enciende por dos caminos que valen lo mismo: uxsm ve el gestor de ventanas
// EWMH, o el propio escritorio ejecuta `uxsm finalize`, como en uwsm. Lo que va
// detrás ―el target de la sesión, graphical-session.target y el autostart― no
// distingue cuál de los dos ha sido.
const ReadyFile = "ready"

// SignalReady enciende la señal de que la sesión está lista. reason dice quién
// la enciende, y queda escrito para el diario.
//
// Devuelve si la ha encendido esta llamada. Un false no es ningún error: es que
// el otro camino llegó antes, y la señal se enciende una sola vez. Eso lo
// decide el sistema, no uxsm: el fichero se crea con O_EXCL, así que de dos
// caminos a la vez sólo puede ganar uno.
func SignalReady(reason string) (bool, error) {
	path, err := readyPath()
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, reason); err != nil {
		return true, err
	}
	return true, nil
}

// Ready dice si la señal está encendida, y con qué razón se encendió.
func Ready() (bool, string, error) {
	path, err := readyPath()
	if err != nil {
		return false, "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, strings.TrimSpace(string(data)), nil
}

// ClearReady apaga la señal. La sesión que empieza lo hace por si quedara
// encendida de una anterior que no llegó a limpiar.
func ClearReady() error {
	path, err := readyPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func readyPath() (string, error) {
	dir, err := RuntimeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ReadyFile), nil
}
