package main

import (
	"fmt"
	"os"

	"github.com/gosuda/x402-facilitator/api/client"
	"github.com/gosuda/x402-facilitator/types"
	"github.com/rs/zerolog/log"
)

func main() {
	// Check for help flag manually before koanf processing
	for _, arg := range os.Args[1:] {
		if arg == "-h" || arg == "--help" || arg == "-help" {
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

	// Validate required fields
	if config.From == "" || config.To == "" || config.Amount == "" || config.PrivateKey == "" {
		fmt.Fprintf(os.Stderr, "Error: Required flags missing\n")
		fmt.Fprintf(os.Stderr, "Usage: %s --from <addr> --to <addr> --amount <amt> --privateKey <key> [options]\n\n", os.Args[0])
		printUsage()
		os.Exit(1)
	}

	// TODO: Implement SDK-based client
	// This requires implementing ClientEvmSigner interface from x402 SDK
	// and using types.NewExactEvmScheme() to create payment payload

	log.Info().
		Str("url", config.URL).
		Str("scheme", config.Scheme).
		Str("network", config.Network).
		Str("token", config.Token).
		Str("from", config.From).
		Str("to", config.To).
		Str("amount", config.Amount).
		Msg("x402-client SDK implementation pending")

	log.Info().Msg("This client will use x402 SDK's ClientEvmSigner to create V2 payment payloads")

	// Example of what this should do:
	// 1. Create a ClientEvmSigner implementation
	// 2. Use types.NewExactEvmScheme(signer, config) to create scheme
	// 3. Call scheme.CreatePaymentPayload() to generate V2 payload
	// 4. Send to facilitator via api/client

	_, _ = client.NewClient(config.URL)
	_ = types.X402VersionV2
}
