package main

import (
	"os"
	"strconv"

	"github.com/gosuda/x402-facilitator/types"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

// Config holds the application configuration
type Config struct {
	Scheme     types.Scheme `mapstructure:"scheme"`
	Network    string       `mapstructure:"network"`
	Port       int          `mapstructure:"port"`
	Url        string       `mapstructure:"url"`
	PrivateKey string       `mapstructure:"privateKey"`
}

// LoadConfig loads configuration from multiple sources (in order of priority):
// 1. Command line flags (highest priority)
// 2. Environment variables (X402_*)
// 3. Configuration file
// 4. Default values (lowest priority)
func LoadConfig() (*Config, error) {
	var k = koanf.New(".")

	// Set default values
	k.Set("scheme", "evm")
	k.Set("network", "base-sepolia")
	k.Set("port", 9090)

	// Define pflags
	f := pflag.NewFlagSet("config", pflag.ContinueOnError)
	f.String("config", "config.toml", "Path to configuration file")
	f.String("scheme", "evm", "Payment scheme (evm, solana, sui, tron)")
	f.String("network", "base-sepolia", "Blockchain network")
	f.Int("port", 9090, "Server port")
	f.String("url", "", "RPC endpoint URL")
	f.String("privateKey", "", "Private key for signing (hex)")

	// Parse flags
	if err := f.Parse(os.Args[1:]); err != nil {
		return nil, err
	}

	// Get config file path from flags
	configPath, _ := f.GetString("config")

	// Load from config file (if exists)
	if _, err := os.Stat(configPath); err == nil {
		if err := k.Load(file.Provider(configPath), toml.Parser()); err != nil {
			return nil, err
		}
	}

	// Load from environment variables (X402_*)
	// Example: X402_SCHEME=evm X402_NETWORK=base-sepolia
	if err := k.Load(env.Provider("X402_", ".", func(s string) string {
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
	println("Usage: x402-facilitator [options]")
	println()
	println("x402-facilitator - Payment facilitator server for x402 protocol")
	println()
	println("Options:")
	println("  --config string")
	println("        Path to configuration file (default \"config.toml\")")
	println("  --scheme string")
	println("        Payment scheme (evm, solana, sui, tron) (default \"evm\")")
	println("  --network string")
	println("        Blockchain network (default \"base-sepolia\")")
	println("  --port int")
	println("        Server port (default 9090)")
	println("  --url string")
	println("        RPC endpoint URL")
	println("  --privateKey string")
	println("        Private key for signing (hex)")
	println("  -h, --help")
	println("        Show this help message")
	println()
	println("Configuration priority (highest to lowest):")
	println("  1. Command line flags")
	println("  2. Environment variables (X402_*)")
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
