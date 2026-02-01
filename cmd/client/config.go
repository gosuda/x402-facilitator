package main

import (
	"os"
	"strconv"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

// Config holds the client configuration
type Config struct {
	URL        string `mapstructure:"url"`
	Scheme     string `mapstructure:"scheme"`
	Network    string `mapstructure:"network"`
	Token      string `mapstructure:"token"`
	From       string `mapstructure:"from"`
	To         string `mapstructure:"to"`
	Amount     string `mapstructure:"amount"`
	PrivateKey string `mapstructure:"privateKey"`
}

// LoadConfig loads configuration from multiple sources (in order of priority):
// 1. Command line flags (highest priority)
// 2. Environment variables (X402_CLIENT_*)
// 3. Configuration file
// 4. Default values (lowest priority)
func LoadConfig() (*Config, error) {
	var k = koanf.New(".")

	// Set default values
	k.Set("url", "http://localhost:9090")
	k.Set("scheme", "evm")
	k.Set("network", "base-sepolia")
	k.Set("token", "USDC")

	// Define pflags
	f := pflag.NewFlagSet("config", pflag.ContinueOnError)
	f.String("config", "", "Path to configuration file")
	f.String("url", "http://localhost:9090", "Base URL of the facilitator server")
	f.String("scheme", "evm", "Scheme to use (evm, solana, sui, tron)")
	f.String("network", "base-sepolia", "Blockchain network (CAIP-2 format)")
	f.String("token", "USDC", "Token contract address")
	f.String("from", "", "Sender address")
	f.String("to", "", "Recipient address")
	f.String("amount", "", "Amount to send (in atomic units)")
	f.String("privateKey", "", "Sender private key (hex)")

	// Parse flags
	if err := f.Parse(os.Args[1:]); err != nil {
		return nil, err
	}

	// Get config file path from flags (if provided)
	configPath, _ := f.GetString("config")
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			if err := k.Load(file.Provider(configPath), toml.Parser()); err != nil {
				return nil, err
			}
		}
	}

	// Load from environment variables (X402_CLIENT_*)
	// Example: X402_CLIENT_URL=http://localhost:9090 X402_CLIENT_FROM=0x...
	if err := k.Load(env.Provider("X402_CLIENT_", ".", func(s string) string {
		return s
	}), nil); err != nil {
		return nil, err
	}

	// Load from command line flags (highest priority)
	if err := k.Load(posflag.Provider(f, ".", k), nil); err != nil {
		return nil, err
	}

	// Unmarshal to struct
	var config Config
	if err := k.Unmarshal("", &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// printUsage prints usage information
func printUsage() {
	println("Usage: x402-client [options]")
	println()
	println("x402-client - Payment client for x402 protocol")
	println()
	println("Options:")
	println("  --config string")
	println("        Path to configuration file")
	println("  --url string")
	println("        Base URL of the facilitator server (default \"http://localhost:9090\")")
	println("  --scheme string")
	println("        Scheme to use: evm, solana, sui, tron (default \"evm\")")
	println("  --network string")
	println("        Blockchain network (CAIP-2 format) (default \"base-sepolia\")")
	println("  --token string")
	println("        Token contract address (default \"USDC\")")
	println("  --from string")
	println("        Sender address")
	println("  --to string")
	println("        Recipient address")
	println("  --amount string")
	println("        Amount to send (in atomic units)")
	println("  --privateKey string")
	println("        Sender private key (hex)")
	println("  -h, --help")
	println("        Show this help message")
	println()
	println("Configuration priority (highest to lowest):")
	println("  1. Command line flags")
	println("  2. Environment variables (X402_CLIENT_*)")
	println("  3. Configuration file")
	println("  4. Default values")
}

// GetEnvOrDefault gets environment variable or returns default value
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetEnvIntOrDefault gets environment variable as int or returns default value
func GetEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
