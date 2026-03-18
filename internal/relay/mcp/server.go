package mcp

import (
	"context"
	"encoding/json"
	"net/http"
)

// Handler processes MCP tool calls.
type Handler interface {
	HandleToolCall(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error)
}

// Server is an MCP Streamable HTTP server.
type Server struct {
	tools   []Tool
	handler Handler
	info    ServerInfo
}

// NewServer creates an MCP server with the given tools and handler.
func NewServer(tools []Tool, handler Handler) *Server {
	return &Server{
		tools:   tools,
		handler: handler,
		info:    ServerInfo{Name: "keep-mcp-relay", Version: "dev"},
	}
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPC(w, JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      0,
			Error:   &JSONRPCError{Code: -32700, Message: "Parse error"},
		})
		return
	}

	var resp JSONRPCResponse
	resp.JSONRPC = "2.0"
	resp.ID = req.ID

	switch req.Method {
	case "initialize":
		resp.Result = InitializeResult{
			ProtocolVersion: "2025-03-26",
			Capabilities: ServerCapabilities{
				Tools: &ToolsCapability{},
			},
			ServerInfo: s.info,
		}

	case "notifications/initialized":
		// Client sends this after initialize. No response needed for notifications,
		// but since we're doing request/response, just return empty success.
		resp.Result = map[string]any{}

	case "tools/list":
		resp.Result = ListToolsResult{Tools: s.tools}

	case "tools/call":
		// Parse params
		paramsBytes, _ := json.Marshal(req.Params)
		var params ToolCallParams
		if err := json.Unmarshal(paramsBytes, &params); err != nil {
			resp.Error = &JSONRPCError{Code: -32602, Message: "Invalid params"}
			break
		}
		result, err := s.handler.HandleToolCall(r.Context(), params.Name, params.Arguments)
		if err != nil {
			resp.Error = &JSONRPCError{Code: -32000, Message: err.Error()}
			break
		}
		resp.Result = result

	default:
		resp.Error = &JSONRPCError{Code: -32601, Message: "Method not found: " + req.Method}
	}

	writeJSONRPC(w, resp)
}

func writeJSONRPC(w http.ResponseWriter, resp JSONRPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
