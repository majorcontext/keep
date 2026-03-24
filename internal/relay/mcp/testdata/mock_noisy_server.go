//go:build ignore

// mock_noisy_server is a mock MCP server that emits async notifications
// between responses, simulating real servers like @modelcontextprotocol/server-sqlite.
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

type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}

		switch req.Method {
		case "notifications/initialized":
			continue

		case "initialize":
			writeResponse(req.ID, map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "mock-noisy", "version": "0.1.0"},
			})
			// Send an async log notification right after initialize response.
			writeNotification("notifications/message", map[string]any{
				"level": "info",
				"data":  "Server initialized successfully",
			})

		case "tools/list":
			// Send a notification BEFORE the actual response.
			writeNotification("notifications/message", map[string]any{
				"level": "debug",
				"data":  "Listing tools...",
			})
			writeResponse(req.ID, map[string]any{
				"tools": []map[string]any{
					{
						"name":        "read_query",
						"description": "Execute a SELECT query",
						"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
					},
				},
			})

		case "tools/call":
			writeResponse(req.ID, map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "result"},
				},
			})
		}
	}
}

func writeResponse(id any, result any) {
	data, _ := json.Marshal(response{JSONRPC: "2.0", ID: id, Result: result})
	fmt.Fprintf(os.Stdout, "%s\n", data)
}

func writeNotification(method string, params any) {
	data, _ := json.Marshal(notification{JSONRPC: "2.0", Method: method, Params: params})
	fmt.Fprintf(os.Stdout, "%s\n", data)
}
