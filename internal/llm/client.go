package llm

import (
	"context"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

// Session holds one conversation with the API. History lives only in memory;
// it dies with the sidebar.
type Session struct {
	client    anthropic.Client
	model     anthropic.Model
	maxTokens int64
	system    []anthropic.TextBlockParam
	messages  []anthropic.MessageParam
}

// NewSession creates a session. Credentials resolve from ANTHROPIC_API_KEY,
// ANTHROPIC_AUTH_TOKEN, or an `ant auth login` profile.
func NewSession(model string, maxTokens int, systemPrompt string) *Session {
	return &Session{
		client:    anthropic.NewClient(),
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
func (s *Session) Ask(ctx context.Context, text string, onDelta func(string)) (stopReason string, err error) {
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

// Explain turns API errors into a one-line human message.
func Explain(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch apierr.StatusCode {
		case 401:
			return "authentication failed — set ANTHROPIC_API_KEY or run `ant auth login`"
		case 404:
			return "unknown model — check `model` in ~/.config/rducky/config.yaml"
		case 429:
			return "rate limited — wait a moment and try again"
		case 500, 529:
			return "the API is having trouble — try again shortly"
		default:
			return fmt.Sprintf("API error (%d): %v", apierr.StatusCode, apierr)
		}
	}
	return err.Error()
}
