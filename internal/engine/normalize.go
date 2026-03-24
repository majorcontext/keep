package engine

import "strings"

// deepLowerStrings returns a shallow copy of the map with all string values
// recursively lowercased. Map keys are preserved as-is. Non-string values
// (ints, bools, floats) are copied unchanged.
func deepLowerStrings(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = lowerValue(v)
	}
	return out
}

// lowerValue lowercases a string, recurses into maps and slices,
// and returns all other types unchanged.
func lowerValue(v any) any {
	switch val := v.(type) {
	case string:
		return strings.ToLower(val)
	case map[string]any:
		return deepLowerStrings(val)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = lowerValue(item)
		}
		return out
	default:
		return v
	}
}

// lowerContext lowercases all string values in the context map,
// including label keys and values.
func lowerContext(ctx map[string]any) map[string]any {
	out := make(map[string]any, len(ctx))
	for k, v := range ctx {
		switch val := v.(type) {
		case string:
			out[k] = strings.ToLower(val)
		case map[string]string:
			lowered := make(map[string]string, len(val))
			for lk, lv := range val {
				lowered[strings.ToLower(lk)] = strings.ToLower(lv)
			}
			out[k] = lowered
		default:
			out[k] = v
		}
	}
	return out
}
