package llm

import (
	"context"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type anthropicSession struct {
	client    anthropic.Client
	model     anthropic.Model
	maxTokens int64
	system    []anthropic.TextBlockParam
	messages  []anthropic.MessageParam
}

// newAnthropicSession creates a session. With no overrides, credentials
// resolve from ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, or an
// `ant auth login` profile.
func newAnthropicSession(model string, maxTokens int, systemPrompt, baseURL, keyEnv string) *anthropicSession {
	var opts []option.RequestOption
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if keyEnv != "" {
		if key := os.Getenv(keyEnv); key != "" {
			opts = append(opts, option.WithAPIKey(key))
		}
	}
	return &anthropicSession{
		client:    anthropic.NewClient(opts...),
		model:     anthropic.Model(model),
		maxTokens: int64(maxTokens),
		system: []anthropic.TextBlockParam{{
			Text:         systemPrompt,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
	}
}

// Ask sends text as the next user turn and streams the reply through onDelta.
// On error (including cancellation) the pending user turn is dropped so the
// conversation history stays valid for the next question.
func (s *anthropicSession) Ask(ctx context.Context, text string, onDelta func(string)) (stopReason string, err error) {
	s.messages = append(s.messages, anthropic.NewUserMessage(anthropic.NewTextBlock(text)))

	stream := s.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     s.model,
		MaxTokens: s.maxTokens,
		System:    s.system,
		Messages:  s.messages,
	})

	var message anthropic.Message
	for stream.Next() {
		event := stream.Current()
		_ = message.Accumulate(event)
		switch ev := event.AsAny().(type) {
		case anthropic.ContentBlockDeltaEvent:
			if d, ok := ev.Delta.AsAny().(anthropic.TextDelta); ok {
				onDelta(d.Text)
			}
		}
	}
	if err := stream.Err(); err != nil {
		s.messages = s.messages[:len(s.messages)-1]
		return "", err
	}

	s.messages = append(s.messages, message.ToParam())
	return string(message.StopReason), nil
}
