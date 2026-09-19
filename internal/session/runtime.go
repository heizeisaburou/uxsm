package session

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Estos ficheros viven en RuntimeDir y permiten pasar el entorno, y con un
// comando también la orden, desde `uxsm start` a los servicios de systemd que
// se ejecutan después y que no heredan directamente el entorno del display
// manager.
//
// Este mecanismo sigue el usado por uwsm: upstream también guarda el entorno
// de login en `env_login` dentro de su directorio de runtime. UXSM añade
// `env_identity` para separar las variables de identidad calculadas por start.
//
// El servicio de entorno crea además sus propios ficheros de estado para poder
// restaurar y limpiar el entorno al cerrar la sesión.
const (
	// LoginFile es el entorno con el que el display manager lanzó la sesión,
	// pasado por FilterEnv (función de este archivo)
	LoginFile = "env_login"
	// IdentityFile son las variables de identidad que ha calculado uxsm start:
	// lo que devuelve IdentityVars (función en identity.go).
	IdentityFile = "env_identity"
	// CommandFile es la línea de órdenes del escritorio cuando la sesión se
	// arranca con un comando (`uxsm start -- bspwm`) en vez de con una entrada:
	// uxsm aux exec la lee de aquí. Va en el mismo formato que los entornos, un
	// argumento detrás de otro separados por un carácter nulo.
	CommandFile = "command"
)

// RuntimeDir es $XDG_RUNTIME_DIR/uxsm: el directorio de trabajo de la sesión.
//
// XDG_RUNTIME_DIR lo crea logind al abrir la sesión y lo borra al cerrar la
// última, así que lo que se deja aquí no sobrevive al usuario.
func RuntimeDir() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(base) {
		return "", errors.New("XDG_RUNTIME_DIR is not set to an absolute path")
	}
	return filepath.Join(base, "uxsm"), nil
}

// varName es un nombre de variable de entorno válido en una shell.
var varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// dropVars son variables propias de la shell o del proceso, no de la sesión.
var dropVars = map[string]bool{"_": true, "SHELL": true, "PWD": true, "OLDPWD": true}

// FilterEnv se queda con las asignaciones "NOMBRE=valor" que son de la sesión:
// quita las que no tienen un nombre válido y las propias de la shell (_, SHELL,
// PWD, OLDPWD). Es el mismo filtro que aplica uwsm a su entorno de login.
func FilterEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		name, _, ok := strings.Cut(kv, "=")
		if ok && varName.MatchString(name) && !dropVars[name] {
			out = append(out, kv)
		}
	}
	return out
}

// WriteEnvFile guarda env en path, una asignación detrás de otra separadas por
// un carácter nulo: es el único carácter que no puede aparecer en un valor.
//
// Escribe primero un fichero temporal y lo renombra, para que quien lo lea no
// encuentre nunca uno a medias. Sólo el usuario puede leerlo.
func WriteEnvFile(path string, env []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	data := strings.Join(env, "\x00")
	if err := os.WriteFile(tmp, []byte(data), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadEnvFile lee un fichero escrito con WriteEnvFile.
func ReadEnvFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	return strings.Split(string(data), "\x00"), nil
}
