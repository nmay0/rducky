package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// openaiSession speaks the OpenAI chat-completions protocol, which every
// non-Anthropic provider in the registry (and most others) accepts.
type openaiSession struct {
	baseURL             string
	keyEnv              string // "" = endpoint needs no auth
	apiKey              string
	model               string
	maxTokens           int
	maxCompletionTokens bool
	messages            []chatMessage // [0] is the system prompt
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.status, e.message) }

func newOpenAISession(baseURL, keyEnv, model string, maxTokens int, systemPrompt string, maxCompletionTokens bool) *openaiSession {
	key := ""
	if keyEnv != "" {
		key = os.Getenv(keyEnv)
	}
	return &openaiSession{
		baseURL:             baseURL,
		keyEnv:              keyEnv,
		apiKey:              key,
		model:               model,
		maxTokens:           maxTokens,
		maxCompletionTokens: maxCompletionTokens,
		messages:            []chatMessage{{Role: "system", Content: systemPrompt}},
	}
}

// Ask sends text as the next user turn and streams the reply through onDelta.
// On error (including cancellation) the pending user turn is dropped so the
// conversation history stays valid for the next question.
func (s *openaiSession) Ask(ctx context.Context, text string, onDelta func(string)) (stopReason string, err error) {
	if s.keyEnv != "" && s.apiKey == "" {
		return "", fmt.Errorf("no API key — export %s (or set `api_key_env` in ~/.config/rducky/config.yaml)", s.keyEnv)
	}

	s.messages = append(s.messages, chatMessage{Role: "user", Content: text})
	reply, finish, err := s.stream(ctx, onDelta)
	if err != nil {
		s.messages = s.messages[:len(s.messages)-1]
		if ctx.Err() != nil {
			return "", context.Canceled
		}
		return "", err
	}
	s.messages = append(s.messages, chatMessage{Role: "assistant", Content: reply})
	return finish, nil
}

func (s *openaiSession) stream(ctx context.Context, onDelta func(string)) (reply, finish string, err error) {
	body := map[string]any{
		"model":    s.model,
		"messages": s.messages,
		"stream":   true,
	}
	if s.maxTokens > 0 {
		if s.maxCompletionTokens {
			body["max_completion_tokens"] = s.maxTokens
		} else {
			body["max_tokens"] = s.maxTokens
		}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		msg := strings.TrimSpace(string(raw))
		var parsed struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &parsed) == nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
		return "", "", &apiError{status: resp.StatusCode, message: msg}
	}

	var out strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Choices) == 0 {
			continue
		}
		c := chunk.Choices[0]
		if c.Delta.Content != "" {
			out.WriteString(c.Delta.Content)
			onDelta(c.Delta.Content)
		}
		if c.FinishReason != nil && *c.FinishReason != "" {
			finish = *c.FinishReason
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", err
	}
	return out.String(), finish, nil
}
