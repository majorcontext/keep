package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ANSI escape codes for verbose output.
const (
	vBold  = "\033[1m"
	vDim   = "\033[2m"
	vRed   = "\033[31m"
	vGreen = "\033[32m"
	vCyan  = "\033[36m"
	vReset = "\033[0m"
)

// defaultStringLimitValue is the default max length for string values in JSON output.
const defaultStringLimitValue = 120

// DefaultStringLimit returns the default string truncation length for verbose output.
func DefaultStringLimit() int { return defaultStringLimitValue }

// VerboseWriter writes human-readable packet dumps to an io.Writer (typically stderr).
type VerboseWriter struct {
	w           io.Writer
	stringLimit int // max chars for string values; 0 = no truncation
}

// NewVerboseWriter creates a verbose writer. If stringLimit > 0, individual
// string values in JSON are truncated to that length. Pass 0 for no truncation.
func NewVerboseWriter(w io.Writer, stringLimit int) *VerboseWriter {
	return &VerboseWriter{w: w, stringLimit: stringLimit}
}

// RequestRaw logs the inbound request, showing metadata as a header and messages pretty-printed.
func (v *VerboseWriter) RequestRaw(body []byte) {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		fmt.Fprintf(v.w, "\n%s%s──▶ REQUEST%s\n", vBold, vCyan, vReset)
		v.writePretty(body)
		return
	}

	meta := v.requestMeta(obj)
	fmt.Fprintf(v.w, "\n%s%s──▶ REQUEST%s  %s%s%s\n", vBold, vCyan, vReset, vDim, meta, vReset)
	v.writeMessages(obj)
}

// RequestAfterPolicy logs the request after policy mutations.
func (v *VerboseWriter) RequestAfterPolicy(body []byte, rule string) {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		label := "after policy"
		if rule != "" {
			label += ": " + rule
		}
		fmt.Fprintf(v.w, "%s%s──▶ REQUEST (%s)%s\n", vBold, vCyan, label, vReset)
		v.writePretty(body)
		return
	}

	label := "after policy"
	if rule != "" {
		label += ": " + rule
	}
	fmt.Fprintf(v.w, "%s%s──▶ REQUEST (%s)%s\n", vBold, vCyan, label, vReset)
	v.writeMessages(obj)
}

// RequestDenied logs that a request was denied by policy.
func (v *VerboseWriter) RequestDenied(rule, message string) {
	fmt.Fprintf(v.w, "%s%s──▶ REQUEST DENIED%s  %s%s: %s%s\n",
		vBold, vRed, vReset, vDim, rule, message, vReset)
}

// RequestAllowed logs that the request passed policy unchanged.
func (v *VerboseWriter) RequestAllowed() {
	fmt.Fprintf(v.w, "%s%s──▶ REQUEST (policy: allow)%s\n", vBold, vGreen, vReset)
}

// ResponseRaw logs the upstream response, showing metadata as a header and content pretty-printed.
func (v *VerboseWriter) ResponseRaw(body []byte) {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		fmt.Fprintf(v.w, "\n%s%s◀── RESPONSE%s\n", vBold, vCyan, vReset)
		v.writePretty(body)
		return
	}

	meta := v.responseMeta(obj)
	fmt.Fprintf(v.w, "\n%s%s◀── RESPONSE%s  %s%s%s\n", vBold, vCyan, vReset, vDim, meta, vReset)
	v.writeContent(obj)
}

// ResponseDenied logs that a response was denied by policy.
func (v *VerboseWriter) ResponseDenied(rule, message string) {
	fmt.Fprintf(v.w, "%s%s◀── RESPONSE DENIED%s  %s%s: %s%s\n",
		vBold, vRed, vReset, vDim, rule, message, vReset)
}

// ResponseAfterPolicy logs the response after policy mutations.
func (v *VerboseWriter) ResponseAfterPolicy(body []byte, rule string) {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		label := "after policy"
		if rule != "" {
			label += ": " + rule
		}
		fmt.Fprintf(v.w, "%s%s◀── RESPONSE (%s)%s\n", vBold, vCyan, label, vReset)
		v.writePretty(body)
		return
	}

	label := "after policy"
	if rule != "" {
		label += ": " + rule
	}
	fmt.Fprintf(v.w, "%s%s◀── RESPONSE (%s)%s\n", vBold, vCyan, label, vReset)
	v.writeContent(obj)
}

// ResponseAllowed logs that the response passed policy unchanged.
func (v *VerboseWriter) ResponseAllowed() {
	fmt.Fprintf(v.w, "%s%s◀── RESPONSE (policy: allow)%s\n\n", vBold, vGreen, vReset)
}

// requestMeta extracts a compact metadata string from a request object.
func (v *VerboseWriter) requestMeta(obj map[string]any) string {
	var parts []string
	if model, ok := obj["model"].(string); ok {
		parts = append(parts, "model="+model)
	}
	if stream, ok := obj["stream"].(bool); ok && stream {
		parts = append(parts, "stream=true")
	}
	return strings.Join(parts, " ")
}

// responseMeta extracts a compact metadata string from a response object.
func (v *VerboseWriter) responseMeta(obj map[string]any) string {
	var parts []string
	if model, ok := obj["model"].(string); ok {
		parts = append(parts, "model="+model)
	}
	if sr, ok := obj["stop_reason"].(string); ok {
		parts = append(parts, "stop_reason="+sr)
	}
	return strings.Join(parts, " ")
}

// writeMessages extracts and pretty-prints the "messages" array from a request.
func (v *VerboseWriter) writeMessages(obj map[string]any) {
	messages, ok := obj["messages"]
	if !ok {
		return
	}
	b, err := json.Marshal(messages)
	if err != nil {
		return
	}
	v.writePretty(b)
}

// writeContent extracts and pretty-prints the "content" array from a response.
func (v *VerboseWriter) writeContent(obj map[string]any) {
	content, ok := obj["content"]
	if !ok {
		return
	}
	b, err := json.Marshal(content)
	if err != nil {
		return
	}
	v.writePretty(b)
}

// writePretty parses JSON, truncates string values, and pretty-prints with dim styling.
func (v *VerboseWriter) writePretty(body []byte) {
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		fmt.Fprintf(v.w, "    %s%s%s\n", vDim, string(body), vReset)
		return
	}

	if v.stringLimit > 0 {
		raw = truncateStrings(raw, v.stringLimit)
	}

	pretty, err := json.MarshalIndent(raw, "    ", "  ")
	if err != nil {
		fmt.Fprintf(v.w, "    %s%s%s\n", vDim, string(body), vReset)
		return
	}

	for _, line := range strings.Split(string(pretty), "\n") {
		fmt.Fprintf(v.w, "    %s%s%s\n", vDim, line, vReset)
	}
}

// truncateStrings recursively walks a JSON value and truncates string values
// that exceed maxLen, appending "…" to indicate truncation.
func truncateStrings(v any, maxLen int) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[k] = truncateStrings(child, maxLen)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, child := range val {
			out[i] = truncateStrings(child, maxLen)
		}
		return out
	case string:
		if len(val) > maxLen {
			return val[:maxLen] + "…"
		}
		return val
	default:
		return v
	}
}
