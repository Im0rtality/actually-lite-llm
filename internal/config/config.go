package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ProviderConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"`
}

type ModelAlias struct {
	Provider              string  `yaml:"provider"`
	Model                 string  `yaml:"model"`
	CostPerMillionInput   float64 `yaml:"cost_per_million_input"`
	CostPerMillionOutput  float64 `yaml:"cost_per_million_output"`
}

type RoutingRule struct {
	Prefix   string `yaml:"prefix"`
	Provider string `yaml:"provider"`
}

type APIKey struct {
	Key           string   `yaml:"key"`
	App           string   `yaml:"app"`
	AllowedModels []string `yaml:"allowed_models"`
}

type Config struct {
	Listen    string                    `yaml:"listen"`
	Providers map[string]ProviderConfig `yaml:"providers"`
	Models    map[string]ModelAlias     `yaml:"models"`
	Routing   []RoutingRule             `yaml:"routing"`
	APIKeys   []APIKey                  `yaml:"api_keys"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	setDefaults(&cfg)
	return &cfg, nil
}

func setDefaults(cfg *Config) {
	if cfg.Listen == "" {
		cfg.Listen = ":8080"
	}
	if p, ok := cfg.Providers["openai"]; ok && p.BaseURL == "" {
		p.BaseURL = "https://api.openai.com/v1"
		cfg.Providers["openai"] = p
	}
	if p, ok := cfg.Providers["anthropic"]; ok && p.BaseURL == "" {
		p.BaseURL = "https://api.anthropic.com/v1"
		cfg.Providers["anthropic"] = p
	}
}

func validate(cfg *Config) error {
	for i, k := range cfg.APIKeys {
		if k.Key == "" {
			return fmt.Errorf("api_keys[%d]: key is required", i)
		}
		if k.App == "" {
			return fmt.Errorf("api_keys[%d]: app is required", i)
		}
	}
	for name, alias := range cfg.Models {
		if alias.Provider == "" {
			return fmt.Errorf("models[%s]: provider is required", name)
		}
		if alias.Model == "" {
			return fmt.Errorf("models[%s]: model is required", name)
		}
	}
	return nil
}
