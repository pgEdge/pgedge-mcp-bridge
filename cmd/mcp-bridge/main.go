// Package main provides the CLI entry point for the MCP HTTP bridge.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pgEdge/pgedge-mcp-bridge/internal/client"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/config"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/logging"
	"github.com/pgEdge/pgedge-mcp-bridge/internal/server"
	"github.com/pgEdge/pgedge-mcp-bridge/pkg/version"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Define command line flags
	var (
		configPath  string
		showVersion bool
		showHelp    bool
	)

	flag.StringVar(&configPath, "config", "", "Path to configuration file")
	flag.StringVar(&configPath, "c", "", "Path to configuration file (shorthand)")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.BoolVar(&showVersion, "v", false, "Print version and exit (shorthand)")
	flag.BoolVar(&showHelp, "help", false, "Print help and exit")
	flag.BoolVar(&showHelp, "h", false, "Print help and exit (shorthand)")

	// Custom usage function
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "MCP HTTP Bridge - Bridge between MCP stdio servers and HTTP clients")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Options:")
		fmt.Fprintln(os.Stderr, "  -c, --config <path>  Path to configuration file")
		fmt.Fprintln(os.Stderr, "                       (default: looks in current dir, then executable dir)")
		fmt.Fprintln(os.Stderr, "  -v, --version        Print version and exit")
		fmt.Fprintln(os.Stderr, "  -h, --help           Print help and exit")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  mcp-bridge                         # Uses config.yaml in current or executable dir")
		fmt.Fprintln(os.Stderr, "  mcp-bridge -c /path/to/config.yaml")
		fmt.Fprintln(os.Stderr, "  mcp-bridge --version")
		fmt.Fprintln(os.Stderr, "  mcp-bridge --help")
	}

	flag.Parse()

	// Handle help flag
	if showHelp {
		flag.Usage()
		return 0
	}

	// Handle version flag
	if showVersion {
		fmt.Println(version.Info())
		return 0
	}

	// Find configuration file
	var err error
	if configPath == "" {
		configPath, err = config.FindConfigFile()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintln(os.Stderr, "Use -c or --config to specify a configuration file")
			return 1
		}
	}

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		return 1
	}

	// Set up logging
	logger, err := logging.NewLogger(cfg.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting up logging: %v\n", err)
		return 1
	}
	defer logger.Close()

	// Set as default logger for package-level logging functions
	logging.SetDefault(logger)

	logger.Info("Starting MCP bridge",
		"version", version.Short(),
		"config", configPath,
		"mode", string(cfg.Mode),
	)

	// Create context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Channel to receive errors from the running service
	errCh := make(chan error, 1)

	// Shutdown function, set based on mode
	var shutdown func(context.Context) error

	// Start the appropriate mode
	switch cfg.Mode {
	case config.ModeServer:
		if cfg.Server == nil {
			logger.Error("Server configuration is missing for server mode")
			return 1
		}

		srv, err := server.NewServer(cfg.Server, logger)
		if err != nil {
			logger.Error("Failed to create server", "error", err)
			return 1
		}

		shutdown = srv.Stop

		go func() {
			errCh <- srv.Start(ctx)
		}()

	case config.ModeClient:
		if cfg.Client == nil {
			logger.Error("Client configuration is missing for client mode")
			return 1
		}

		cli, err := client.NewClient(cfg.Client, logger)
		if err != nil {
			logger.Error("Failed to create client", "error", err)
			return 1
		}

		shutdown = func(ctx context.Context) error {
			return cli.Close()
		}

		go func() {
			errCh <- cli.Run(ctx)
		}()

	default:
		logger.Error("Unknown mode", "mode", string(cfg.Mode))
		return 1
	}

	// Wait for shutdown signal or error
	select {
	case sig := <-sigCh:
		logger.Info("Received shutdown signal", "signal", sig.String())

		// Create a timeout context for shutdown
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		// Cancel the main context to signal services to stop
		cancel()

		// Call the shutdown function
		if err := shutdown(shutdownCtx); err != nil {
			logger.Error("Error during shutdown", "error", err)
		}

		// Wait for the service goroutine to finish
		select {
		case err := <-errCh:
			if err != nil && err != context.Canceled {
				logger.Error("Service error during shutdown", "error", err)
				return 1
			}
		case <-shutdownCtx.Done():
			logger.Warn("Shutdown timed out")
			return 1
		}

	case err := <-errCh:
		if err != nil && err != context.Canceled {
			logger.Error("Service error", "error", err)
			return 1
		}
	}

	logger.Info("MCP bridge stopped")
	return 0
}
