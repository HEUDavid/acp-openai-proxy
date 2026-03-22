package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sashabaranov/go-openai"
	"github.com/yourname/acp-openai-proxy/internal/backend"
)

type Handler struct {
	registry *backend.Registry
}

func SetupRouter(registry *backend.Registry) *gin.Engine {
	r := gin.Default()

	handler := &Handler{registry: registry}

	r.GET("/v1/models", handler.handleModels)
	r.POST("/v1/chat/completions", handler.handleChatCompletions)

	return r
}

func (h *Handler) handleModels(c *gin.Context) {
	modelNames := h.registry.AllModels()
	models := make([]openai.Model, 0, len(modelNames))
	for _, name := range modelNames {
		models = append(models, openai.Model{ID: name, Object: "model", OwnedBy: "system"})
	}
	c.JSON(http.StatusOK, openai.ModelsList{Models: models})
}

func (h *Handler) handleChatCompletions(c *gin.Context) {
	var req openai.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Invalid JSON mapping"}})
		return
	}

	if req.Model == "" {
		req.Model = "gemini-3-flash-preview"
	}

	b, err := h.registry.Resolve(req.Model)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}

	streamCh := make(chan backend.StreamEvent, 100)

	go func() {
		err := b.Chat(c.Request.Context(), req.Model, req.Messages, streamCh)
		if err != nil && err != context.Canceled {
			log.Printf("[api] backend chat error: %v", err)
		}
	}()

	if req.Stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")

		streamResponse(c, streamCh, req.Model)
		return
	}

	fullText := collectFullResponse(c, streamCh)
	c.JSON(http.StatusOK, openai.ChatCompletionResponse{
		ID:     "chatcmpl-backend",
		Object: "chat.completion",
		Model:  req.Model,
		Choices: []openai.ChatCompletionChoice{
			{
				Index: 0,
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: fullText,
				},
			},
		},
	})
}

func streamResponse(c *gin.Context, streamCh <-chan backend.StreamEvent, model string) {
	c.Stream(func(w io.Writer) bool {
		select {
		case msg, ok := <-streamCh:
			if !ok || msg.Type == "done" {
				c.SSEvent("", "[DONE]")
				return false
			}

			if msg.Type == "error" {
				log.Printf("backend stream error: %s", msg.Content)
				c.SSEvent("", "[DONE]")
				return false
			}

			if msg.Type == "text" && msg.Content != "" {
				delta := openai.ChatCompletionStreamResponse{
					ID:     "stream",
					Object: "chat.completion.chunk",
					Model:  model,
					Choices: []openai.ChatCompletionStreamChoice{
						{Delta: openai.ChatCompletionStreamChoiceDelta{Role: openai.ChatMessageRoleAssistant, Content: msg.Content}},
					},
				}
				c.SSEvent("", delta)
			} else if msg.Type == "tool_call" {
				delta := openai.ChatCompletionStreamResponse{
					ID:     "stream",
					Object: "chat.completion.chunk",
					Model:  model,
					Choices: []openai.ChatCompletionStreamChoice{
						{Delta: openai.ChatCompletionStreamChoiceDelta{Role: openai.ChatMessageRoleAssistant, Content: fmt.Sprintf("\n> 🔨 Tool Call: %s\n", msg.Content)}},
					},
				}
				c.SSEvent("", delta)
			}
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}

func collectFullResponse(c *gin.Context, streamCh <-chan backend.StreamEvent) string {
	var sb strings.Builder
	for {
		select {
		case msg, ok := <-streamCh:
			if !ok || msg.Type == "done" || msg.Type == "error" {
				return sb.String()
			}
			if msg.Type == "text" {
				sb.WriteString(msg.Content)
			} else if msg.Type == "tool_call" {
				sb.WriteString(fmt.Sprintf("\n> 🔨 Tool Call: %s\n", msg.Content))
			}
		case <-c.Request.Context().Done():
			return sb.String()
		}
	}
}
