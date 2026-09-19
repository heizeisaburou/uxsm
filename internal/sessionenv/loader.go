package sessionenv

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/heizeisaburou/uxsm/internal/session"
)

// loaderScript es loader.sh, metido en el binario al compilar: así no hay que
// instalar un script aparte ni buscarlo en tiempo de ejecución.
//
//go:embed loader.sh
var loaderScript string

// auxPrefix marca las variables auxiliares que uxsm le pasa al cargador. No son
// de la sesión y se quitan del resultado.
const auxPrefix = "__UXSM_"

// runLoader ejecuta loader.sh con el entorno base y la identidad de la sesión, y
// devuelve el entorno que queda después de cargar el perfil y los ficheros de
// entorno.
//
// Los ficheros de entorno son scripts de shell ―pueden calcular valores, no sólo
// asignarlos―, así que la única forma de saber qué dejan es ejecutarlos en una
// shell y leer el entorno al final. Es lo que hace uwsm con su prepare-env.sh.
func runLoader(base, identity []string) ([]string, error) {
	mark, err := randomMark()
	if err != nil {
		return nil, err
	}

	env := append([]string(nil), base...)
	for _, kv := range identity {
		name, value, _ := strings.Cut(kv, "=")
		env = append(env, auxPrefix+name+"__="+value)
	}
	env = append(env, auxPrefix+"MARK__="+mark)

	cmd := exec.Command("/bin/sh", "-c", loaderScript, "uxsm-loader")
	cmd.Env = env
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("loading the session environment: %w", err)
	}

	messages, dump, err := splitAtMark(out, mark)
	if err != nil {
		return nil, err
	}
	if len(messages) > 0 {
		os.Stdout.Write(messages)
	}
	return parseEnvDump(dump), nil
}

// randomMark es la marca que separa los mensajes del cargador de su entorno. Es
// aleatoria para que ningún mensaje ni fichero de entorno pueda contenerla.
func randomMark() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// splitAtMark separa la salida del cargador en los mensajes de antes de la marca
// y el volcado del entorno de detrás.
//
// Busca la primera aparición: la marca vuelve a salir dentro del volcado, en la
// propia variable __UXSM_MARK__.
func splitAtMark(out []byte, mark string) (messages, dump []byte, err error) {
	i := bytes.Index(out, []byte(mark))
	if i < 0 {
		return nil, nil, fmt.Errorf("the environment loader did not print its mark; output: %q", out)
	}
	return out[:i], out[i+len(mark):], nil
}

// parseEnvDump lee la salida de `env -0`: asignaciones separadas por caracteres
// nulos. Quita las variables auxiliares y las que no son de la sesión.
func parseEnvDump(dump []byte) []string {
	var env []string
	for _, kv := range strings.Split(strings.TrimRight(string(dump), "\x00"), "\x00") {
		if kv == "" || strings.HasPrefix(kv, auxPrefix) {
			continue
		}
		env = append(env, kv)
	}
	return session.FilterEnv(env)
}
