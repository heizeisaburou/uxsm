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

// loaderScript is loader.sh embedded in the binary at build time, avoiding a
// separate installed script and runtime lookup.
//
//go:embed loader.sh
var loaderScript string

// auxPrefix identifies auxiliary variables uxsm passes to the loader. They are
// not session variables and are removed from the result.
const auxPrefix = "__UXSM_"

// runLoader runs loader.sh with the base environment and session identity, then
// returns the environment left after loading the profile and environment files.
//
// Environment files are shell scripts—they may compute values rather than only
// assign them—so the only way to know their result is to run them in a shell and
// read the final environment. This is what uwsm does with prepare-env.sh.
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

// randomMark is the marker separating loader messages from its environment. It
// is random so no message or environment file can contain it.
func randomMark() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// splitAtMark divides loader output into messages before the marker and the
// environment dump after it.
//
// It uses the first occurrence because the marker appears again inside the dump
// in the __UXSM_MARK__ variable itself.
func splitAtMark(out []byte, mark string) (messages, dump []byte, err error) {
	i := bytes.Index(out, []byte(mark))
	if i < 0 {
		return nil, nil, fmt.Errorf("the environment loader did not print its mark; output: %q", out)
	}
	return out[:i], out[i+len(mark):], nil
}

// parseEnvDump reads `env -0` output: null-separated assignments. It removes
// auxiliary variables and variables unrelated to the session.
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
