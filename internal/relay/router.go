package relay

import (
	"context"
	"fmt"
	"log"
	"os"

	relayconfig "github.com/majorcontext/keep/internal/relay/config"
	"github.com/majorcontext/keep/internal/relay/mcp"
)

// ToolRoute maps a tool to its upstream client and scope.
type ToolRoute struct {
	Client *mcp.Client
	Scope  string
	Tool   mcp.Tool
}

// Router maps tool names to upstream MCP clients and scopes.
type Router struct {
	routes map[string]*ToolRoute // keyed by tool name
	tools  []mcp.Tool            // merged list of all tools
}

// NewRouter connects to all configured upstreams, discovers tools,
// and builds a routing table. Returns an error if any tool name
// appears in multiple upstreams.
func NewRouter(ctx context.Context, routes []relayconfig.Route) (*Router, error) {
	router := &Router{routes: make(map[string]*ToolRoute)}

	for _, route := range routes {
		// Validate auth env vars are set
		if route.Auth != nil && route.Auth.TokenEnv != "" {
			if os.Getenv(route.Auth.TokenEnv) == "" {
				return nil, fmt.Errorf("relay: route %q: auth env var %q is not set", route.Scope, route.Auth.TokenEnv)
			}
		}

		// Build client options from auth config
		var opts []mcp.ClientOption
		if route.Auth != nil {
			switch route.Auth.Type {
			case "bearer":
				opts = append(opts, mcp.WithBearerToken(route.Auth.TokenEnv))
			case "header":
				opts = append(opts, mcp.WithHeader(route.Auth.Header, route.Auth.TokenEnv))
			}
		}

		client := mcp.NewClient(route.Upstream, opts...)

		// Initialize
		_, err := client.Initialize(ctx)
		if err != nil {
			log.Printf("relay: upstream %q unreachable: %v (skipping)", route.Upstream, err)
			continue
		}

		// Discover tools
		tools, err := client.ListTools(ctx)
		if err != nil {
			log.Printf("relay: upstream %q tool discovery failed: %v (skipping)", route.Upstream, err)
			continue
		}

		// Register tools in routing table
		for _, tool := range tools {
			if existing, ok := router.routes[tool.Name]; ok {
				return nil, fmt.Errorf("relay: tool %q registered by both scope %q and scope %q", tool.Name, existing.Scope, route.Scope)
			}
			router.routes[tool.Name] = &ToolRoute{
				Client: client,
				Scope:  route.Scope,
				Tool:   tool,
			}
			router.tools = append(router.tools, tool)
		}
	}

	return router, nil
}

// Lookup returns the route for a tool name, or an error if not found.
func (r *Router) Lookup(toolName string) (*ToolRoute, error) {
	route, ok := r.routes[toolName]
	if !ok {
		return nil, fmt.Errorf("relay: unknown tool %q", toolName)
	}
	return route, nil
}

// Tools returns the merged list of all tools from all upstreams.
func (r *Router) Tools() []mcp.Tool {
	return r.tools
}
