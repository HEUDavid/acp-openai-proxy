package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/sashabaranov/go-openai"
	"github.com/valyala/fastjson"
	"github.com/yourname/acp-openai-proxy/internal/backend"
)

type GeminiBackend struct {
	manager *Manager
}

func NewGeminiBackend(manager *Manager) *GeminiBackend {
	return &GeminiBackend{
		manager: manager,
	}
}

func (gb *GeminiBackend) Name() string {
	return "gemini"
}

func (gb *GeminiBackend) Models() []string {
	return []string{
		"gemini-3.1-pro-preview",
		"gemini-3-flash-preview",
		"gemini-2.5-pro",
	}
}

func (gb *GeminiBackend) Chat(ctx context.Context, model string, messages []openai.ChatCompletionMessage, stream chan<- backend.StreamEvent) error {
	defer close(stream)

	worker, err := gb.manager.GetWorker(model)
	if err != nil {
		return fmt.Errorf("failed to start or get ACP worker: %v", err)
	}

	session, err := worker.CreateSession(ctx, "/tmp")
	if err != nil {
		return fmt.Errorf("failed to create session: %v", err)
	}
	defer session.Close()

	promptBlocks := parseMessagesToBlocks(messages)
	if len(promptBlocks) == 0 {
		return fmt.Errorf("no valid messages provided")
	}

	log.Printf("[gemini] dispatching prompt to %s | sessionId: %s", model, session.ID)

	streamCh := worker.GetProcess().SubscribeStream(session.ID)

	chErr := make(chan error, 1)
	go func() {
		_, err := worker.GetProcess().Agent.Prompt(context.Background(), acpsdk.PromptRequest{
			SessionId: acpsdk.SessionId(session.ID),
			Prompt:    promptBlocks,
		})
		if err != nil {
			chErr <- err
		}
		close(chErr)
	}()

	for {
		select {
		case err := <-chErr:
			if err != nil {
				log.Printf("[gemini] prompt error: %v", err)
				stream <- backend.StreamEvent{Type: "error", Content: err.Error()}
			}
			stream <- backend.StreamEvent{Type: "done", Content: ""}
			return err
		case msg, ok := <-streamCh:
			if !ok {
				stream <- backend.StreamEvent{Type: "done", Content: ""}
				return nil
			}

			// Using fastjson for dynamic union type extraction to avoid strict type assertions
			b, _ := json.Marshal(msg.Update)
			val, _ := fastjson.ParseBytes(b)
			text, updateType := extractUpdateText(val)

			if updateType == "agent_message_chunk" && text != "" {
				stream <- backend.StreamEvent{Type: "text", Content: text}
			} else if updateType == "tool_call" {
				title := string(val.GetStringBytes("title"))
				stream <- backend.StreamEvent{Type: "tool_call", Content: title}
			}
		case <-ctx.Done():
			stream <- backend.StreamEvent{Type: "done"}
			return ctx.Err()
		}
	}
}

func parseMessagesToBlocks(msgs []openai.ChatCompletionMessage) []acpsdk.ContentBlock {
	var blocks []acpsdk.ContentBlock
	for _, m := range msgs {
		if m.Role == openai.ChatMessageRoleSystem {
			mime := "text/plain"
			blocks = append(blocks, acpsdk.ResourceBlock(acpsdk.EmbeddedResourceResource{
				TextResourceContents: &acpsdk.TextResourceContents{
					Uri:      "context://embedded",
					MimeType: &mime,
					Text:     m.Content,
				},
			}))
			continue
		}

		if m.Content != "" {
			blocks = append(blocks, acpsdk.TextBlock(fmt.Sprintf("[%s]: %s\n", m.Role, m.Content)))
		}

		for _, p := range m.MultiContent {
			if p.Type == openai.ChatMessagePartTypeText {
				blocks = append(blocks, acpsdk.TextBlock(fmt.Sprintf("[%s]: %s\n", m.Role, p.Text)))
			} else if p.Type == openai.ChatMessagePartTypeImageURL && p.ImageURL != nil {
				uri := p.ImageURL.URL
				cut, found := strings.CutPrefix(uri, "data:")
				if found {
					semicolonIdx := strings.Index(cut, ";")
					commaIdx := strings.Index(cut, ",")
					if semicolonIdx >= 0 && commaIdx >= 0 && commaIdx > semicolonIdx {
						blocks = append(blocks, acpsdk.ImageBlock(
							cut[commaIdx+1:],
							cut[:semicolonIdx],
						))
					}
				}
			}
		}
	}
	return blocks
}

func extractUpdateText(val *fastjson.Value) (text, updateType string) {
	if val == nil {
		return "", ""
	}

	updateType = string(val.GetStringBytes("sessionUpdate"))
	if updateType != "agent_message_chunk" {
		return "", updateType
	}

	contentVal := val.Get("content")
	if contentVal.Type() == fastjson.TypeString {
		text = string(contentVal.GetStringBytes())
	} else {
		text = string(contentVal.GetStringBytes("text"))
	}
	return text, updateType
}
