package guardy

import "errors"

// ExecutionScope provides typed lookup for policy and decode phases.
// Host applications implement this or use [MapScope].
type ExecutionScope interface {
	Lookup(key string) (value any, ok bool)
}

// MapScope is a simple in-memory [ExecutionScope].
type MapScope map[string]any

// Lookup implements [ExecutionScope].
func (m MapScope) Lookup(key string) (any, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m[key]
	return v, ok
}

// ErrScopeIncomplete is returned when [Pipeline.Run] is called without required scope keys.
var ErrScopeIncomplete = errors.New("guardy: execution scope incomplete")

func mergeRequiredKeys(existing []string, keys []string) []string {
	if len(keys) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing)+len(keys))
	out := make([]string, 0, len(existing)+len(keys))
	for _, k := range existing {
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	for _, k := range keys {
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

func checkScopeComplete(scope ExecutionScope, requiredKeys []string) error {
	if len(requiredKeys) == 0 {
		return nil
	}
	if scope == nil {
		scope = MapScope{}
	}
	for _, key := range requiredKeys {
		if _, ok := scope.Lookup(key); !ok {
			return ErrScopeIncomplete
		}
	}
	return nil
}
