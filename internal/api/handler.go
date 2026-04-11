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

func authMiddleware(apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if apiKey == "" {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "Missing or invalid Authorization header"}})
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != apiKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "Invalid API key"}})
			return
		}

		c.Next()
	}
}

func SetupRouter(registry *backend.Registry, apiKey string) *gin.Engine {
	r := gin.Default()

	r.Use(authMiddleware(apiKey))

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

	fullText := collectFullResponse(c.Request.Context(), streamCh)
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

// eventContent 将 StreamEvent 转为用户可见的文本内容，统一 text 和 tool_call 的格式化逻辑
func eventContent(msg backend.StreamEvent) string {
	switch msg.Type {
	case "text":
		return msg.Content
	case "tool_call":
		return fmt.Sprintf("\n> 🔨 Tool Call: %s\n", msg.Content)
	default:
		return ""
	}
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

			if content := eventContent(msg); content != "" {
				delta := openai.ChatCompletionStreamResponse{
					ID:     "stream",
					Object: "chat.completion.chunk",
					Model:  model,
					Choices: []openai.ChatCompletionStreamChoice{
						{Delta: openai.ChatCompletionStreamChoiceDelta{Role: openai.ChatMessageRoleAssistant, Content: content}},
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

func collectFullResponse(ctx context.Context, streamCh <-chan backend.StreamEvent) string {
	var sb strings.Builder
	for {
		select {
		case msg, ok := <-streamCh:
			if !ok || msg.Type == "done" || msg.Type == "error" {
				return sb.String()
			}
			sb.WriteString(eventContent(msg))
		case <-ctx.Done():
			return sb.String()
		}
	}
}
