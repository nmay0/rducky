// Package llm holds one chat session against a provider. Anthropic uses the
// official SDK; every other provider speaks the OpenAI chat-completions
// protocol through the same hand-rolled client (openai.go).
package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Options selects and configures a provider session.
type Options struct {
	Provider  string // registry name; "" means anthropic
	Model     string // "" means the provider's default
	BaseURL   string // override endpoint; required for provider "custom"
	APIKeyEnv string // override env var holding the API key
	MaxTokens int
	System    string
}

type asker interface {
	Ask(ctx context.Context, text string, onDelta func(string)) (stopReason string, err error)
}

// Session is one in-memory conversation; it dies with the sidebar.
type Session struct {
	Provider string
	Model    string
	asker
}

// Provider registry. kind "openai" means OpenAI-compatible chat completions.
type provider struct {
	Name         string
	BaseURL      string
	KeyEnv       string // "" = no auth required (ollama)
	DefaultModel string
	anthropic    bool
	// OpenAI proper rejects max_tokens on reasoning models; it wants
	// max_completion_tokens. Most compat endpoints only know max_tokens.
	maxCompletionTokens bool
}

var providers = []provider{
	{Name: "anthropic", KeyEnv: "ANTHROPIC_API_KEY", DefaultModel: "claude-opus-4-8", anthropic: true},
	{Name: "openai", BaseURL: "https://api.openai.com/v1", KeyEnv: "OPENAI_API_KEY", DefaultModel: "gpt-5.1", maxCompletionTokens: true},
	{Name: "gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", KeyEnv: "GEMINI_API_KEY", DefaultModel: "gemini-2.5-flash"},
	{Name: "xai", BaseURL: "https://api.x.ai/v1", KeyEnv: "XAI_API_KEY", DefaultModel: "grok-4"},
	{Name: "groq", BaseURL: "https://api.groq.com/openai/v1", KeyEnv: "GROQ_API_KEY", DefaultModel: "llama-3.3-70b-versatile"},
	{Name: "cerebras", BaseURL: "https://api.cerebras.ai/v1", KeyEnv: "CEREBRAS_API_KEY", DefaultModel: "llama-3.3-70b"},
	{Name: "mistral", BaseURL: "https://api.mistral.ai/v1", KeyEnv: "MISTRAL_API_KEY", DefaultModel: "mistral-large-latest"},
	{Name: "deepseek", BaseURL: "https://api.deepseek.com/v1", KeyEnv: "DEEPSEEK_API_KEY", DefaultModel: "deepseek-chat"},
	{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", KeyEnv: "OPENROUTER_API_KEY", DefaultModel: "openai/gpt-5.1"},
	{Name: "ollama", BaseURL: "http://localhost:11434/v1", DefaultModel: "llama3.2"},
}

var aliases = map[string]string{"claude": "anthropic", "google": "gemini", "grok": "xai"}

// Table returns the registry for display (`rducky providers`).
func Table() []provider { return providers }

func names() string {
	var n []string
	for _, p := range providers {
		n = append(n, p.Name)
	}
	return strings.Join(append(n, "custom"), ", ")
}

// NewSession resolves the provider and returns a ready session. It does not
// hit the network; a missing API key surfaces on the first Ask so the REPL
// stays alive to show the fix.
func NewSession(o Options) (*Session, error) {
	name := strings.ToLower(strings.TrimSpace(o.Provider))
	if name == "" {
		name = "anthropic"
	}
	if a, ok := aliases[name]; ok {
		name = a
	}

	var p provider
	switch {
	case name == "custom":
		if o.BaseURL == "" {
			return nil, fmt.Errorf("provider `custom` needs `base_url` in ~/.config/rducky/config.yaml")
		}
		if o.Model == "" {
			return nil, fmt.Errorf("provider `custom` needs `model` in ~/.config/rducky/config.yaml")
		}
		p = provider{Name: "custom"}
	default:
		found := false
		for _, cand := range providers {
			if cand.Name == name {
				p, found = cand, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown provider %q — valid: %s", o.Provider, names())
		}
	}

	model := o.Model
	if model == "" {
		model = p.DefaultModel
	}
	baseURL := strings.TrimRight(o.BaseURL, "/")
	if baseURL == "" {
		baseURL = p.BaseURL
	}
	keyEnv := o.APIKeyEnv
	if keyEnv == "" {
		keyEnv = p.KeyEnv
	}

	var impl asker
	if p.anthropic {
		impl = newAnthropicSession(model, o.MaxTokens, o.System, o.BaseURL, o.APIKeyEnv)
	} else {
		impl = newOpenAISession(baseURL, keyEnv, model, o.MaxTokens, o.System, p.maxCompletionTokens)
	}
	return &Session{Provider: name, Model: model, asker: impl}, nil
}

// Explain turns API errors into a one-line human message.
func Explain(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var anthErr *anthropic.Error
	if errors.As(err, &anthErr) {
		switch anthErr.StatusCode {
		case 401:
			return "authentication failed — set ANTHROPIC_API_KEY or run `ant auth login`"
		case 404:
			return "unknown model — check `model` in ~/.config/rducky/config.yaml"
		case 429:
			return "rate limited — wait a moment and try again"
		case 500, 529:
			return "the API is having trouble — try again shortly"
		default:
			return fmt.Sprintf("API error (%d): %v", anthErr.StatusCode, anthErr)
		}
	}
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		switch apiErr.status {
		case 401, 403:
			return "authentication failed — check your API key"
		case 404:
			return "not found — check `model` (and `base_url`) in ~/.config/rducky/config.yaml"
		case 429:
			return "rate limited — wait a moment and try again"
		case 500, 502, 503, 529:
			return "the provider is having trouble — try again shortly"
		default:
			return fmt.Sprintf("API error (%d): %s", apiErr.status, apiErr.message)
		}
	}
	return err.Error()
}
