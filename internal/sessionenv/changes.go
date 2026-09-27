package sessionenv

import (
	"sort"
	"strings"
)

// envMap converts "NAME=value" assignments into a map. The last value wins
// when a name is repeated.
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

// assignments converts a map into "NAME=value" assignments sorted by name so
// the result does not depend on map iteration order.
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

// changes describes what preparation must do in the manager.
type changes struct {
	// set contains variables to import in "NAME=value" form.
	set []string
	// unset contains variables to remove.
	unset []string
	// cleanup contains names to remove when the session ends.
	cleanup []string
}

// computeChanges compares the manager snapshot (pre) with the environment left
// by the loader (post), following uwsm's prepare_env rules:
//
//   - Import values changed or added in post, plus alwaysExport values present
//     in post, excluding neverExport and alwaysUnset.
//   - Remove values present in the manager but absent from post, plus
//     alwaysUnset values present in the manager.
//   - On shutdown, remove imported values except neverCleanup.
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

// cleanupNames decides what to remove when the session ends, following uwsm's
// cleanup_env rules: names recorded during preparation plus alwaysCleanup,
// excluding neverCleanup, that are currently in the manager and were absent
// from the snapshot. Snapshot values are restored afterwards to their old values.
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
