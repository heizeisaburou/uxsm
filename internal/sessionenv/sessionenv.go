package sessionenv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/heizeisaburou/uxsm/internal/session"
	"github.com/heizeisaburou/uxsm/internal/systemd"
)

// Ficheros de trabajo en session.RuntimeDir, además de los que deja uxsm start.
const (
	// preFile es la foto del gestor antes de preparar la sesión.
	preFile = "env_pre"
	// cleanupFile son los nombres que hay que borrar al cerrar.
	cleanupFile = "env_cleanup"
)

// Prepare monta el entorno de la sesión en el gestor. Es `uxsm aux prepare-env`,
// el ExecStart= de uxsm-env@.service, y corre antes que el escritorio.
//
//  1. Guarda la foto del entorno del gestor.
//  2. Ejecuta el cargador con la foto, el entorno de login por encima y la
//     identidad de la sesión, y obtiene el entorno resultante.
//  3. Calcula qué subir y qué borrar (computeChanges), apunta lo que habrá que
//     borrar al cerrar, y lo aplica en el gestor y, si hace falta, en D-Bus.
//
// Apunta la limpieza antes de tocar el gestor: si algo falla a medias, la
// limpieza sabe qué deshacer.
func Prepare() error {
	dir, err := session.RuntimeDir()
	if err != nil {
		return err
	}
	login, err := session.ReadEnvFile(filepath.Join(dir, session.LoginFile))
	if err != nil {
		return fmt.Errorf("reading the login environment saved by uxsm start: %w", err)
	}
	identity, err := session.ReadEnvFile(filepath.Join(dir, session.IdentityFile))
	if err != nil {
		return fmt.Errorf("reading the session identity saved by uxsm start: %w", err)
	}

	// La foto se filtra igual que el entorno resultante: si no, lo que el filtro
	// quita del resultado (SHELL…) parecería desaparecido y se borraría del gestor.
	pre, err := systemd.Environment()
	if err != nil {
		return err
	}
	pre = session.FilterEnv(pre)
	if err := session.WriteEnvFile(filepath.Join(dir, preFile), pre); err != nil {
		return fmt.Errorf("saving the systemd environment snapshot: %w", err)
	}

	// El entorno de login pisa al del gestor, igual que en uwsm.
	base := assignments(mergeEnv(envMap(pre), envMap(login)))
	post, err := runLoader(base, identity)
	if err != nil {
		return err
	}

	c := computeChanges(pre, post)
	if err := session.WriteEnvFile(filepath.Join(dir, cleanupFile), c.cleanup); err != nil {
		return fmt.Errorf("saving the cleanup list: %w", err)
	}

	fmt.Printf("Exporting to the systemd user manager: %v\n", names(c.set))
	if err := systemd.SetEnvironment(c.set...); err != nil {
		return err
	}
	if len(c.unset) > 0 {
		fmt.Printf("Removing from the systemd user manager: %v\n", c.unset)
		if err := systemd.UnsetEnvironment(c.unset...); err != nil {
			return err
		}
	}
	if !systemd.DBusIsBroker() {
		if err := systemd.UpdateDBusActivationEnvironment(c.set); err != nil {
			fmt.Fprintf(os.Stderr, "uxsm: updating the D-Bus activation environment: %v\n", err)
		}
	}
	return nil
}

// Cleanup deja el entorno del gestor como estaba antes de la sesión. Es `uxsm
// aux cleanup-env`, el ExecStopPost= de uxsm-env@.service, así que corre al
// parar el servicio por cualquier motivo, también si la preparación falló.
//
//  1. Borra lo que decida cleanupNames.
//  2. Restaura la foto.
//  3. Borra los ficheros de trabajo de la sesión.
//
// Sin foto no hay nada que deshacer: la preparación no llegó a empezar.
func Cleanup() error {
	dir, err := session.RuntimeDir()
	if err != nil {
		return err
	}
	pre, err := session.ReadEnvFile(filepath.Join(dir, preFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	marked, err := session.ReadEnvFile(filepath.Join(dir, cleanupFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	now, err := systemd.Environment()
	if err != nil {
		return err
	}

	toUnset := cleanupNames(pre, now, marked)
	if len(toUnset) > 0 {
		fmt.Printf("Removing from the systemd user manager: %v\n", toUnset)
		if !systemd.DBusIsBroker() {
			empty := make([]string, len(toUnset))
			for i, n := range toUnset {
				empty[i] = n + "="
			}
			if err := systemd.UpdateDBusActivationEnvironment(empty); err != nil {
				fmt.Fprintf(os.Stderr, "uxsm: clearing the D-Bus activation environment: %v\n", err)
			}
		}
		if err := systemd.UnsetEnvironment(toUnset...); err != nil {
			return err
		}
	}

	fmt.Println("Restoring the systemd user manager environment from before the session.")
	if err := systemd.SetEnvironment(pre...); err != nil {
		return err
	}

	for _, f := range []string{preFile, cleanupFile, session.LoginFile, session.IdentityFile, session.CommandFile} {
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// mergeEnv devuelve base con las variables de over por encima.
func mergeEnv(base, over map[string]string) map[string]string {
	m := make(map[string]string, len(base)+len(over))
	for k, v := range base {
		m[k] = v
	}
	for k, v := range over {
		m[k] = v
	}
	return m
}

// names son los nombres de unas asignaciones, para los mensajes.
func names(env []string) []string {
	out := make([]string, 0, len(env))
	for name := range envMap(env) {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
