package relay

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	relayconfig "github.com/majorcontext/keep/internal/relay/config"
	"github.com/majorcontext/keep/internal/relay/mcp"
)

// ToolRoute maps a tool to its upstream client and scope.
type ToolRoute struct {
	Client mcp.ToolCaller
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

		upstreamLabel := route.Upstream
		if route.Command != "" {
			upstreamLabel = route.Command
		}

		// Create and initialize client.
		// Stdio upstreams (e.g. npx/uvx) may need time to install packages,
		// so retry with a fresh subprocess each attempt.
		var client mcp.ToolCaller
		var initErr error
		maxAttempts := 1
		if route.Command != "" {
			maxAttempts = 15 // ~30s total for stdio processes (package install)
		}

		for attempt := range maxAttempts {
			if route.Command != "" {
				stdioClient, err := mcp.NewStdioClient(route.Command, route.Args...)
				if err != nil {
					initErr = err
					if attempt < maxAttempts-1 {
						log.Printf("relay: upstream %q failed to start (attempt %d/%d): %v", upstreamLabel, attempt+1, maxAttempts, err)
						time.Sleep(2 * time.Second)
						continue
					}
					break
				}
				client = stdioClient
			} else {
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
				client = mcp.NewClient(route.Upstream, opts...)
			}

			_, initErr = client.Initialize(ctx)
			if initErr == nil {
				break
			}
			if attempt < maxAttempts-1 {
				log.Printf("relay: upstream %q not ready (attempt %d/%d): %v", upstreamLabel, attempt+1, maxAttempts, initErr)
				// Close the failed stdio client before retrying.
				if closer, ok := client.(interface{ Close() error }); ok {
					_ = closer.Close()
				}
				time.Sleep(2 * time.Second)
			}
		}
		if initErr != nil {
			log.Printf("relay: upstream %q unreachable after %d attempts: %v (skipping)", upstreamLabel, maxAttempts, initErr)
			continue
		}

		// Discover tools
		tools, err := client.ListTools(ctx)
		if err != nil {
			log.Printf("relay: upstream %q tool discovery failed: %v (skipping)", upstreamLabel, err)
			if closer, ok := client.(interface{ Close() error }); ok {
				_ = closer.Close()
			}
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
