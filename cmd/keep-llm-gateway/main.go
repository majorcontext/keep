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
	"github.com/majorcontext/keep/internal/gateway"
	gwconfig "github.com/majorcontext/keep/internal/gateway/config"
)

func main() {
	configPath := flag.String("config", "", "Path to gateway config file (required)")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "error: --config flag is required")
		os.Exit(2)
	}

	// 1. Load gateway config
	cfg, err := gwconfig.Load(*configPath)
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

	// 3. Create audit logger
	logger, closer, err := audit.NewLoggerFromOutput(cfg.Log.Output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating logger: %v\n", err)
		os.Exit(1)
	}
	if closer != nil {
		defer closer.Close()
	}

	// 4. Create proxy
	proxy, err := gateway.NewProxy(engine, cfg, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating proxy: %v\n", err)
		os.Exit(1)
	}

	// 5. Start HTTP server with timeouts
	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           proxy,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second, // longer for LLM responses
		IdleTimeout:       120 * time.Second,
	}

	// 6. Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		httpServer.Shutdown(ctx) //nolint:errcheck
	}()

	log.Printf("keep-llm-gateway listening on %s (provider: %s, scope: %s)", cfg.Listen, cfg.Provider, cfg.Scope)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
