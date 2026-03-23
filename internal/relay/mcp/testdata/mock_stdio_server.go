//go:build ignore

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result"`
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			fmt.Fprintf(os.Stderr, "mock: unmarshal error: %v\n", err)
			continue
		}

		switch req.Method {
		case "notifications/initialized":
			// Notification — no response.
			continue

		case "initialize":
			writeResponse(req.ID, map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "mock-stdio-server",
					"version": "0.1.0",
				},
			})

		case "tools/list":
			writeResponse(req.ID, map[string]any{
				"tools": []map[string]any{
					{
						"name":        "echo",
						"description": "Echoes back the arguments",
						"inputSchema": map[string]any{
							"type":       "object",
							"properties": map[string]any{},
						},
					},
				},
			})

		case "tools/call":
			var params toolCallParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				fmt.Fprintf(os.Stderr, "mock: unmarshal params: %v\n", err)
				continue
			}
			argsJSON, _ := json.Marshal(params.Arguments)
			writeResponse(req.ID, map[string]any{
				"content": []map[string]any{
					{
						"type": "text",
						"text": string(argsJSON),
					},
				},
			})

		default:
			fmt.Fprintf(os.Stderr, "mock: unknown method: %s\n", req.Method)
		}
	}
}

func writeResponse(id any, result any) {
	resp := response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(os.Stdout, "%s\n", data)
}
