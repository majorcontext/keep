package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/majorcontext/keep"
	"github.com/majorcontext/keep/internal/audit"
	"github.com/majorcontext/keep/internal/relay"
	relayconfig "github.com/majorcontext/keep/internal/relay/config"
	"github.com/majorcontext/keep/internal/relay/mcp"
)

func main() {
	configPath := flag.String("config", "", "Path to relay config file (required)")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "error: --config flag is required")
		os.Exit(2)
	}

	// 1. Load relay config
	cfg, err := relayconfig.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// 2. Load Keep engine
	var engineOpts []keep.Option
	if cfg.ProfilesDir != "" {
		engineOpts = append(engineOpts, keep.WithProfilesDir(cfg.ProfilesDir))
	}
	if cfg.PacksDir != "" {
		engineOpts = append(engineOpts, keep.WithPacksDir(cfg.PacksDir))
	}
	engine, err := keep.Load(cfg.RulesDir, engineOpts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading rules: %v\n", err)
		os.Exit(1)
	}
	defer engine.Close()

	// 3. Build router (connect to upstreams, discover tools)
	ctx := context.Background()
	router, err := relay.NewRouter(ctx, cfg.Routes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error building router: %v\n", err)
		os.Exit(1)
	}

	// 4. Create audit logger
	logger, closer, err := audit.NewLoggerFromOutput(cfg.Log.Output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating logger: %v\n", err)
		os.Exit(1)
	}
	if closer != nil {
		defer func() { _ = closer.Close() }()
	}

	// 5. Create handler and MCP server
	handler := relay.NewRelayHandler(engine, router, logger)
	server := mcp.NewServer(router.Tools(), handler)

	// 6. Start HTTP server
	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           server,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 7. Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	reloadCh := make(chan os.Signal, 1)
	signal.Notify(reloadCh, syscall.SIGHUP)

	go func() {
		for range reloadCh {
			log.Println("received SIGHUP, reloading rules (upstream connections unchanged)...")
			if err := engine.Reload(); err != nil {
				log.Printf("reload failed: %v (keeping current rules)", err)
			} else {
				log.Println("rules reloaded successfully")
			}
		}
	}()

	go func() {
		<-sigCh
		log.Println("shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}()

	tools := router.Tools()
	log.Printf("keep-mcp-relay listening on %s (%d tools from %d upstreams)", cfg.Listen, len(tools), len(cfg.Routes))
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
