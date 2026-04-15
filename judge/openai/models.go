package openai

var shortcuts = map[string]string{
	"gpt-4o":      "gpt-4o-2024-11-20",
	"gpt-4o-mini": "gpt-4o-mini-2024-07-18",
	"o3":          "o3-2025-04-16",
}

// ResolveModel expands a model shortcut to its full identifier.
// If the model is not a known shortcut, it is returned as-is.
func ResolveModel(model string) string {
	if full, ok := shortcuts[model]; ok {
		return full
	}
	return model
}
