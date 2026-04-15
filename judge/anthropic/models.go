package anthropic

var shortcuts = map[string]string{
	"haiku":  "claude-haiku-4-5-20251001",
	"sonnet": "claude-sonnet-4-6-20260401",
	"opus":   "claude-opus-4-6-20260401",
}

// ResolveModel maps a shortcut to a full model ID.
// If the input is not a known shortcut, it is returned as-is.
func ResolveModel(model string) string {
	if full, ok := shortcuts[model]; ok {
		return full
	}
	return model
}
