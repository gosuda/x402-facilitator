package main

import (
	"github.com/gosuda/x402-facilitator/types"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	Scheme     types.Scheme `mapstructure:"scheme"`
	Network    string       `mapstructure:"network"`
	Port       int          `mapstructure:"port"`
	Url        string       `mapstructure:"url"`
	PrivateKey string       `mapstructure:"privateKey"`
	// DelegationManager, when set, serves the x402 v2 erc7710 asset-transfer method against this
	// pinned ERC-7710 manager instead of the token-signature paths. See facilitator/erc7710.go.
	DelegationManager string `mapstructure:"delegationManager"`
}

func LoadConfig(path string) (*Config, error) {
	var k = koanf.New(".")

	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		return nil, err
	}
	var config Config
	if err := k.Unmarshal("", &config); err != nil {
		return nil, err
	}
	return &config, nil
}
