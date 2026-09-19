package sessionenv

import (
	"sort"
	"strings"
)

// envMap convierte asignaciones "NOMBRE=valor" en un mapa. Si un nombre se
// repite, gana la última.
func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if ok {
			m[name] = value
		}
	}
	return m
}

// assignments convierte un mapa en asignaciones "NOMBRE=valor", ordenadas por
// nombre para que el resultado no dependa del orden del mapa.
func assignments(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for name, value := range m {
		out = append(out, name+"="+value)
	}
	sort.Strings(out)
	return out
}

func sortedNames(s set) []string {
	out := make([]string, 0, len(s))
	for n := range s {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// changes es lo que la preparación tiene que hacer en el gestor.
type changes struct {
	// set son las variables que se suben, "NOMBRE=valor".
	set []string
	// unset son las que se borran.
	unset []string
	// cleanup son los nombres que habrá que borrar al cerrar la sesión.
	cleanup []string
}

// computeChanges compara la foto del gestor (pre) con el entorno que ha dejado
// el cargador (post), con el criterio de prepare_env de uwsm:
//
//   - Se sube lo que cambia o es nuevo en post, más las de alwaysExport que
//     estén en post, menos las de neverExport y alwaysUnset.
//   - Se borra lo que estaba en el gestor y ya no está en post, más las de
//     alwaysUnset, siempre que el gestor las tenga.
//   - Al cerrar se borrará lo que se sube, menos las de neverCleanup.
func computeChanges(pre, post []string) changes {
	preMap, postMap := envMap(pre), envMap(post)

	toSet := make(map[string]string)
	for name, value := range postMap {
		if old, ok := preMap[name]; !ok || old != value {
			toSet[name] = value
		}
	}
	for name := range alwaysExport {
		if value, ok := postMap[name]; ok {
			toSet[name] = value
		}
	}
	for name := range neverExport.union(alwaysUnset) {
		delete(toSet, name)
	}

	toUnset := make(set)
	for name := range preMap {
		if _, ok := postMap[name]; !ok || alwaysUnset[name] {
			toUnset[name] = true
		}
	}

	cleanup := make(set)
	for name := range toSet {
		if !neverCleanup[name] {
			cleanup[name] = true
		}
	}

	return changes{set: assignments(toSet), unset: sortedNames(toUnset), cleanup: sortedNames(cleanup)}
}

// cleanupNames decide qué borrar al cerrar la sesión, con el criterio de
// cleanup_env de uwsm: lo que apuntó la preparación y las de alwaysCleanup,
// menos las de neverCleanup, que el gestor tenga ahora y que no estuvieran en la
// foto. Lo que estaba en la foto se restaura después con su valor de entonces.
func cleanupNames(pre []string, now []string, marked []string) []string {
	preNames, nowNames := envMap(pre), envMap(now)
	candidates := newSet(marked...).union(alwaysCleanup)

	out := make(set)
	for name := range candidates {
		_, inNow := nowNames[name]
		_, inPre := preNames[name]
		if inNow && !inPre && !neverCleanup[name] {
			out[name] = true
		}
	}
	return sortedNames(out)
}
