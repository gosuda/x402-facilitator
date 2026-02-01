package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gosuda/x402-facilitator/api"
	"github.com/gosuda/x402-facilitator/facilitator"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// Check for help flag manually before koanf processing
	for _, arg := range os.Args[1:] {
		if arg == "-h" || arg == "-help" || arg == "--help" {
			printUsage()
			os.Exit(0)
		}
	}

	// Load configuration
	config, err := LoadConfig()
	if err != nil {
		if err.Error() == "flag: help requested" {
			printUsage()
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Setup logger
	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Caller().Logger()

	// Create facilitator
	fac, err := facilitator.NewFacilitator(config.Scheme, config.Network, config.Url, config.PrivateKey)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to init facilitator, shutting down...")
	}

	// Create API server
	apiServer := api.NewServer(fac)

	// Initialize HTTP server
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.Port),
		Handler: apiServer,
	}

	// Start server in goroutine
	go func() {
		log.Info().Msgf("Starting server on port %d", config.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Failed to start server, shutting down...")
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to shutdown server gracefully")
	}
	log.Info().Msg("Server shutdown gracefully")
}
