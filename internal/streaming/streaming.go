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

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
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

		// Use AWS event stream decoder
		decoder := eventstream.NewDecoder()
		payloadBuf := make([]byte, 0, 64*1024) // 64KB buffer for large payloads
		errorCount := 0
		maxErrors := 10

		for {
			msg, err := decoder.Decode(reader, payloadBuf)
			if err == io.EOF {
				ch <- "data: [DONE]\n\n"
				return
			}
			if err != nil {
				errorCount++
				if errorCount > maxErrors {
					ch <- fmt.Sprintf(`data: {"error":%q}`+"\n\n", "stream decode error limit exceeded: "+err.Error())
					return
				}
				continue
			}

			// Get event type from headers
			eventType := ""
			if h := msg.Headers.Get(":event-type"); h != nil {
				if sv, ok := h.(eventstream.StringValue); ok {
					eventType = string(sv)
				}
			}

			// Get content type
			contentType := ""
			if h := msg.Headers.Get(":content-type"); h != nil {
				if sv, ok := h.(eventstream.StringValue); ok {
					contentType = string(sv)
				}
			}

			// Only process JSON content
			if contentType != "application/json" {
				continue
			}

			// Parse the JSON payload
			var payload map[string]interface{}
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				continue
			}

			// Convert to OpenAI streaming format
			switch eventType {
			case "assistantResponseEvent":
				if content, ok := payload["content"].(string); ok && content != "" {
					// Send initial role on first content chunk
					if handler.firstChunk {
						roleChunk := fmt.Sprintf(`data: {"id":"%s","object":"chat.completion.chunk","created":%d,"model":%q,"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`+"\n\n",
							handler.CompletionID(), handler.created, model)
						ch <- roleChunk
						handler.firstChunk = false
					}
					chunk := fmt.Sprintf(`data: {"id":"%s","object":"chat.completion.chunk","created":%d,"model":%q,"choices":[{"index":0,"delta":{"content":%q},"finish_reason":null}]}`+"\n\n",
						handler.CompletionID(), handler.created, model, content)
					ch <- chunk
				}
			case "messageStopEvent":
				// End of stream
				ch <- "data: [DONE]\n\n"
				return
			case "metadataEvent":
				// Could contain usage info - send final chunk
				ch <- "data: [DONE]\n\n"
				return
			}
		}
	}()

	return ch, nil
}

func CollectResponse(reader io.Reader, model string) (*models.ChatCompletionResponse, error) {
	handler := NewStreamHandler(model)
	var fullContent strings.Builder
	var toolCalls []models.ToolCall
	var finishReason string

	// Use AWS event stream decoder
	decoder := eventstream.NewDecoder()
	payloadBuf := make([]byte, 0, 64*1024) // 64KB buffer for large payloads
	errorCount := 0
	maxErrors := 10

	for {
		msg, err := decoder.Decode(reader, payloadBuf)
		if err == io.EOF {
			if finishReason == "" {
				finishReason = "stop"
			}
			break
		}
		if err != nil {
			errorCount++
			if errorCount > maxErrors {
				return nil, fmt.Errorf("stream decode error limit exceeded: %w", err)
			}
			continue
		}

		// Get event type from headers
		eventType := ""
		if h := msg.Headers.Get(":event-type"); h != nil {
			if sv, ok := h.(eventstream.StringValue); ok {
				eventType = string(sv)
			}
		}

		// Get content type
		contentType := ""
		if h := msg.Headers.Get(":content-type"); h != nil {
			if sv, ok := h.(eventstream.StringValue); ok {
				contentType = string(sv)
			}
		}

		// Only process JSON content
		if contentType != "application/json" {
			continue
		}

		// Parse the JSON payload
		var payload map[string]interface{}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			continue
		}

		switch eventType {
		case "assistantResponseEvent":
			// Handle content
			if content, ok := payload["content"].(string); ok {
				fullContent.WriteString(content)
			}
		case "toolUseEvent":
			// Tool use start
			if name, ok := payload["name"].(string); ok {
				toolCallID := ""
				if id, ok := payload["toolUseId"].(string); ok {
					toolCallID = id
				}
				inputStr := ""
				if input, ok := payload["input"].(string); ok {
					inputStr = input
				} else if inputMap, ok := payload["input"].(map[string]interface{}); ok {
					inputBytes, err := json.Marshal(inputMap)
					if err != nil {
						continue // Skip tool call if marshaling fails
					}
					inputStr = string(inputBytes)
				}
				toolCalls = append(toolCalls, models.ToolCall{
					ID:       toolCallID,
					Type:     "function",
					Function: &models.ToolCallFunction{Name: name, Arguments: inputStr},
				})
			}
		case "messageStopEvent":
			finishReason = "stop"
		case "metadataEvent":
			// Could contain usage info
			if _, ok := payload["usage"]; ok {
				finishReason = "stop"
			}
		}
	}

	resp := &models.ChatCompletionResponse{
		ID:      handler.CompletionID(),
		Object:  "chat.completion",
		Created: handler.created,
		Model:   model,
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
		if resp.Choices[0].FinishReason == "" {
			resp.Choices[0].FinishReason = "tool_calls"
		}
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
