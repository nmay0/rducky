package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Provider     string `yaml:"provider"`    // see `rducky providers`; default anthropic
	Model        string `yaml:"model"`       // empty = the provider's default
	BaseURL      string `yaml:"base_url"`    // endpoint override; required for provider "custom"
	APIKeyEnv    string `yaml:"api_key_env"` // env var holding the key, if not the provider's usual one
	MaxTokens    int    `yaml:"max_tokens"`
	ContextLines int    `yaml:"context_lines"`
	Split        string `yaml:"split"` // "h" = right sidebar, "v" = bottom
	Size         string `yaml:"size"`  // tmux split size, e.g. "35%"
}

func defaults() Config {
	return Config{
		Provider:     "anthropic",
		MaxTokens:    8192,
		ContextLines: 200,
		Split:        "h",
		Size:         "35%",
	}
}

// Load returns defaults overlaid with ~/.config/rducky/config.yaml if it exists.
// A missing or unreadable config file is not an error.
func Load() Config {
	cfg := defaults()

	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return cfg
		}
		dir = filepath.Join(home, ".config")
	}

	data, err := os.ReadFile(filepath.Join(dir, "rducky", "config.yaml"))
	if err != nil {
		return cfg
	}
	var fileCfg Config
	if yaml.Unmarshal(data, &fileCfg) != nil {
		return cfg
	}

	if fileCfg.Provider != "" {
		cfg.Provider = fileCfg.Provider
	}
	if fileCfg.Model != "" {
		cfg.Model = fileCfg.Model
	}
	if fileCfg.BaseURL != "" {
		cfg.BaseURL = fileCfg.BaseURL
	}
	if fileCfg.APIKeyEnv != "" {
		cfg.APIKeyEnv = fileCfg.APIKeyEnv
	}
	if fileCfg.MaxTokens > 0 {
		cfg.MaxTokens = fileCfg.MaxTokens
	}
	if fileCfg.ContextLines > 0 {
		cfg.ContextLines = fileCfg.ContextLines
	}
	if fileCfg.Split == "h" || fileCfg.Split == "v" {
		cfg.Split = fileCfg.Split
	}
	if fileCfg.Size != "" {
		cfg.Size = fileCfg.Size
	}
	return cfg
}
