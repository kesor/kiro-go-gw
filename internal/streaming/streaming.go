package streaming

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"kiro-go-gw/internal/models"
)

type KiroEvent struct {
	Type            string                 `json:"eventType"`
	Content         string                 `json:"content,omitempty"`
	ToolCallID      string                 `json:"toolCallId,omitempty"`
	ToolName        string                 `json:"toolName,omitempty"`
	ToolUse         map[string]interface{} `json:"toolUse,omitempty"`
	CompletionUsage map[string]interface{} `json:"completionUsage,omitempty"`
	EndTurn         *bool                  `json:"endTurn,omitempty"`
}

type StreamHandler struct {
	completionID string
	created      int64
	model        string
	firstChunk   bool
}

func NewStreamHandler(model string) *StreamHandler {
	return &StreamHandler{
		completionID: generateCompletionID(),
		created:      time.Now().Unix(),
		model:        model,
		firstChunk:   true,
	}
}

func (h *StreamHandler) ParseAndConvert(line string) (string, error) {
	line = strings.TrimSpace(line)

	// Skip empty lines and comments
	if line == "" || strings.HasPrefix(line, "#") {
		return "", nil
	}

	// Handle SSE format: "data: {...}"
	if strings.HasPrefix(line, "data: ") {
		line = strings.TrimPrefix(line, "data: ")
	}

	// Handle [DONE] signal
	if line == "[DONE]" {
		return "", io.EOF
	}

	// Parse JSON
	var event KiroEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		// Not JSON, might be other SSE data
		return "", nil
	}

	// Convert to OpenAI format
	return h.convertEvent(event)
}

func (h *StreamHandler) convertEvent(event KiroEvent) (string, error) {
	switch event.Type {
	case "contentBlockDelta":
		return h.handleContentDelta(event)
	case "contentBlockStart":
		return h.handleContentStart(event)
	case "contentBlockStop":
		return h.handleContentStop(event)
	case "toolUse":
		return h.handleToolUse(event)
	case "messageDelta":
		return h.handleMessageDelta(event)
	case "messageStop":
		return h.handleMessageStop(event)
	default:
		return "", nil
	}
}

func (h *StreamHandler) handleContentDelta(event KiroEvent) (string, error) {
	// Build message - only include role on first chunk
	msg := models.ChatMessage{}

	if h.firstChunk {
		msg.Role = "assistant"
		h.firstChunk = false
	}

	if event.Content != "" {
		msg.Content = event.Content
	}

	chunk := models.ChatCompletionChunk{
		ID:      h.completionID,
		Object:  "chat.completion.chunk",
		Created: h.created,
		Model:   h.model,
		Choices: []models.ChunkChoice{
			{
				Index:        0,
				Delta:        &msg,
				FinishReason: "",
			},
		},
	}

	// Handle tool calls in event
	if event.ToolUse != nil {
		msg.ToolCalls = []models.ToolCall{
			{
				ID:   event.ToolCallID,
				Type: "function",
				Function: &models.ToolCallFunction{
					Name:      event.ToolName,
					Arguments: "",
				},
			},
		}
	}

	data, err := json.Marshal(chunk)
	if err != nil {
		return "", err
	}

	return "data: " + string(data) + "\n\n", nil
}

func (h *StreamHandler) handleContentStart(event KiroEvent) (string, error) {
	return "", nil
}

func (h *StreamHandler) handleContentStop(event KiroEvent) (string, error) {
	return "", nil
}

func (h *StreamHandler) handleToolUse(event KiroEvent) (string, error) {
	chunk := models.ChatCompletionChunk{
		ID:      h.completionID,
		Object:  "chat.completion.chunk",
		Created: h.created,
		Model:   h.model,
		Choices: []models.ChunkChoice{
			{
				Index: 0,
				Delta: &models.ChatMessage{
					ToolCalls: []models.ToolCall{
						{
							ID:   event.ToolCallID,
							Type: "function",
							Function: &models.ToolCallFunction{
								Name:      event.ToolName,
								Arguments: "",
							},
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(chunk)
	if err != nil {
		return "", err
	}

	return "data: " + string(data) + "\n\n", nil
}

func (h *StreamHandler) handleMessageDelta(event KiroEvent) (string, error) {
	if event.EndTurn != nil && *event.EndTurn {
		chunk := models.ChatCompletionChunk{
			ID:      h.completionID,
			Object:  "chat.completion.chunk",
			Created: h.created,
			Model:   h.model,
			Choices: []models.ChunkChoice{
				{
					Index:        0,
					Delta:        &models.ChatMessage{Content: ""},
					FinishReason: "stop",
				},
			},
		}

		data, err := json.Marshal(chunk)
		if err != nil {
			return "", err
		}

		return "data: " + string(data) + "\n\n", nil
	}
	return "", nil
}

func (h *StreamHandler) handleMessageStop(event KiroEvent) (string, error) {
	// Return [DONE]
	return "data: [DONE]\n\n", nil
}

func (h *StreamHandler) CompletionID() string {
	return h.completionID
}

func ParseStream(reader io.Reader) <-chan string {
	ch := make(chan string, 10)

	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 0), 1024*1024) // 1MB max token size

		for scanner.Scan() {
			line := scanner.Text()
			ch <- line
		}
		if err := scanner.Err(); err != nil {
			ch <- "data: {\"error\":\"" + err.Error() + "\"}\n\n"
		}
	}()

	return ch
}

func StreamToOpenAI(reader io.Reader, model string) (<-chan string, error) {
	ch := make(chan string, 10)
	handler := NewStreamHandler(model)

	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 0), 1024*1024) // 1MB max token size

		for scanner.Scan() {
			line := scanner.Text()

			output, err := handler.ParseAndConvert(line)
			if err == io.EOF {
				ch <- "data: [DONE]\n\n"
				break
			}
			if err != nil {
				continue
			}
			if output != "" {
				ch <- output
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- "data: {\"error\":\"" + err.Error() + "\"}\n\n"
		}
	}()

	return ch, nil
}

func CollectResponse(reader io.Reader, model string) (*models.ChatCompletionResponse, error) {
	handler := NewStreamHandler(model)
	var fullContent strings.Builder
	var toolCalls []models.ToolCall
	var finishReason string

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0), 1024*1024) // 1MB max token size

	for scanner.Scan() {
		line := scanner.Text()

		output, err := handler.ParseAndConvert(line)
		if err == io.EOF {
			finishReason = "stop"
			break
		}
		if err != nil {
			continue
		}

		// Extract content from the chunk
		if strings.HasPrefix(output, "data: ") {
			data := strings.TrimPrefix(output, "data: ")
			if data == "[DONE]" {
				finishReason = "stop"
				break
			}

			var chunk models.ChatCompletionChunk
			if err := json.Unmarshal([]byte(data), &chunk); err == nil {
				if len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
					if content, ok := chunk.Choices[0].Delta.Content.(string); ok {
						fullContent.WriteString(content)
					}
					if len(chunk.Choices[0].Delta.ToolCalls) > 0 {
						toolCalls = chunk.Choices[0].Delta.ToolCalls
					}
				}
				if len(chunk.Choices) > 0 && chunk.Choices[0].FinishReason != "" {
					finishReason = chunk.Choices[0].FinishReason
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	resp := &models.ChatCompletionResponse{
		ID:      handler.CompletionID(),
		Object:  "chat.completion",
		Created: handler.created,
		Model:   handler.model,
		Choices: []models.Choice{
			{
				Index: 0,
				Message: &models.ChatMessage{
					Role:    "assistant",
					Content: fullContent.String(),
				},
				FinishReason: finishReason,
			},
		},
	}

	if len(toolCalls) > 0 {
		resp.Choices[0].Message.ToolCalls = toolCalls
	}

	return resp, nil
}

func generateCompletionID() string {
	return fmt.Sprintf("chatcmpl-%s", randomHex(16))
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based if crypto rand fails
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)[:n]
}
