package backend

import (
	"context"

	"github.com/sashabaranov/go-openai"
)

// StreamEvent represents a unified stream event from any backend.
type StreamEvent struct {
	Type    string // e.g., "text", "tool_call", "error", "done"
	Content string
}

// Backend is the unified interface for AI CLI engines.
type Backend interface {
	// Name returns the identifier of the backend, e.g., "gemini"
	Name() string

	// Models returns a list of supported model names
	Models() []string

	// Chat handles the entire chat response stream
	Chat(ctx context.Context, model string, messages []openai.ChatCompletionMessage, stream chan<- StreamEvent) error
}
