package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Model        string `yaml:"model"`
	MaxTokens    int    `yaml:"max_tokens"`
	ContextLines int    `yaml:"context_lines"`
	Split        string `yaml:"split"` // "h" = right sidebar, "v" = bottom
	Size         string `yaml:"size"`  // tmux split size, e.g. "35%"
}

func defaults() Config {
	return Config{
		Model:        "claude-opus-4-8",
		MaxTokens:    8192,
		ContextLines: 200,
		Split:        "h",
		Size:         "35%",
	}
}

// Load returns defaults overlaid with ~/.config/qq/config.yaml if it exists.
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

	data, err := os.ReadFile(filepath.Join(dir, "qq", "config.yaml"))
	if err != nil {
		return cfg
	}
	var fileCfg Config
	if yaml.Unmarshal(data, &fileCfg) != nil {
		return cfg
	}

	if fileCfg.Model != "" {
		cfg.Model = fileCfg.Model
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
