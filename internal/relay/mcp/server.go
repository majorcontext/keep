package mcp

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"
)

// Handler processes MCP tool calls.
type Handler interface {
	HandleToolCall(ctx context.Context, name string, args map[string]any) (*ToolCallResult, error)
}

// Server is an MCP Streamable HTTP server.
type Server struct {
	tools       []Tool
	handler     Handler
	info        ServerInfo
	initialized atomic.Bool
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

	r.Body = http.MaxBytesReader(w, r.Body, 4<<20) // 4 MB limit

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPC(w, JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      0,
			Error:   &JSONRPCError{Code: -32700, Message: "Parse error"},
		})
		return
	}

	// Notifications have no id field. Per MCP Streamable HTTP spec,
	// respond with 202 Accepted and no body.
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	var resp JSONRPCResponse
	resp.JSONRPC = "2.0"
	resp.ID = req.ID

	switch req.Method {
	case "initialize":
		resp.Result = InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities: ServerCapabilities{
				Tools: &ToolsCapability{},
			},
			ServerInfo: s.info,
		}
		s.initialized.Store(true)

	case "tools/list":
		if !s.initialized.Load() {
			resp.Error = &JSONRPCError{Code: -32002, Message: "server not initialized"}
			break
		}
		tools := s.tools
		if tools == nil {
			tools = []Tool{}
		}
		resp.Result = ListToolsResult{Tools: tools}

	case "tools/call":
		if !s.initialized.Load() {
			resp.Error = &JSONRPCError{Code: -32002, Message: "server not initialized"}
			break
		}
		// Parse params
		paramsBytes, _ := json.Marshal(req.Params)
		var params ToolCallParams
		if err := json.Unmarshal(paramsBytes, &params); err != nil {
			resp.Error = &JSONRPCError{Code: -32602, Message: "Invalid params"}
			break
		}
		result, err := s.handler.HandleToolCall(r.Context(), params.Name, params.Arguments)
		if err != nil {
			log.Printf("mcp: tool call %q failed: %v", params.Name, err)
			resp.Error = &JSONRPCError{Code: -32000, Message: "tool call failed"}
			break
		}
		resp.Result = result

	default:
		method := req.Method
		if len(method) > 64 {
			method = method[:64] + "..."
		}
		resp.Error = &JSONRPCError{Code: -32601, Message: "Method not found: " + method}
	}

	writeJSONRPC(w, resp)
}

func writeJSONRPC(w http.ResponseWriter, resp JSONRPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
