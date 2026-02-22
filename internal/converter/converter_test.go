package converter

import (
	"encoding/json"
	"strings"
	"testing"

	"kiro-go-gw/internal/models"
)

func TestBuildKiroPayload_Basic(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	// Check conversationState exists
	convState, ok := payload["conversationState"].(map[string]interface{})
	if !ok {
		t.Fatal("conversationState not found in payload")
	}

	// Check conversationId in conversationState
	if convState["conversationId"] != "test-conv-id" {
		t.Errorf("conversationId: got %v, want test-conv-id", convState["conversationId"])
	}

	// Check chatTriggerType
	if convState["chatTriggerType"] != "MANUAL" {
		t.Errorf("chatTriggerType: got %v, want MANUAL", convState["chatTriggerType"])
	}

	// Check currentMessage -> userInputMessage -> modelId
	currentMsg, ok := convState["currentMessage"].(map[string]interface{})
	if !ok {
		t.Fatal("currentMessage not found")
	}
	userInputMsg, ok := currentMsg["userInputMessage"].(map[string]interface{})
	if !ok {
		t.Fatal("userInputMessage not found")
	}
	if userInputMsg["modelId"] != "claude-sonnet-4.5" {
		t.Errorf("modelId: got %v, want claude-sonnet-4.5", userInputMsg["modelId"])
	}
	if userInputMsg["origin"] != "KIRO_CLI" {
		t.Errorf("origin: got %v, want KIRO_CLI", userInputMsg["origin"])
	}
	// Content can be string or array of content blocks
	switch content := userInputMsg["content"].(type) {
	case string:
		if content != "Hello" {
			t.Errorf("content: got %v, want Hello", content)
		}
	case []map[string]interface{}:
		if len(content) != 1 || content[0]["text"] != "Hello" {
			t.Errorf("content: got %v, want [{text: Hello}]", content)
		}
	default:
		t.Fatalf("content has unexpected type: %T", userInputMsg["content"])
	}

	// Check history contains previous messages
	history, ok := convState["history"].([]map[string]interface{})
	if !ok {
		t.Fatalf("history has unexpected type: %T", convState["history"])
	}
	if len(history) != 1 {
		t.Fatalf("history length: got %d, want 1", len(history))
	}
	firstHistoryMsg := history[0]
	if assistantMsg, ok := firstHistoryMsg["assistantResponseMessage"]; ok {
		if am, ok := assistantMsg.(map[string]interface{}); ok {
			if content, ok := am["content"].(string); ok {
				if content != "Hi there!" {
					t.Errorf("history[0].content: got %v, want Hi there!", content)
				}
			}
		}
	}
}

func TestBuildKiroPayload_SystemPrompt(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hello"},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	// System prompt should be extracted
	if payload["systemPrompt"] != "You are a helpful assistant." {
		t.Errorf("systemPrompt: got %v, want 'You are a helpful assistant.'", payload["systemPrompt"])
	}
}

func TestBuildKiroPayload_MultipleSystemPrompts(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "system", Content: "First system prompt."},
			{Role: "system", Content: "Second system prompt."},
			{Role: "user", Content: "Hello"},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	// Should be concatenated
	want := "First system prompt.\nSecond system prompt."
	if payload["systemPrompt"] != want {
		t.Errorf("systemPrompt: got %q, want %q", payload["systemPrompt"], want)
	}
}

func TestBuildKiroPayload_Tools(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "Use the calculator"},
		},
		Tools: []models.Tool{
			{
				Type: "function",
				Function: &models.ToolFunction{
					Name:        "calculator",
					Description: "Perform calculations",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"expression": map[string]string{"type": "string"},
						},
					},
				},
			},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	// Tools should be in userInputMessageContext
	convState := payload["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInputMsg := currentMsg["userInputMessage"].(map[string]interface{})

	ctx, ok := userInputMsg["userInputMessageContext"].(map[string]interface{})
	if !ok {
		t.Fatal("userInputMessageContext not found")
	}

	tools, ok := ctx["tools"].([]map[string]interface{})
	if !ok {
		t.Fatal("tools not found in userInputMessageContext")
	}

	if len(tools) != 1 {
		t.Errorf("tool count: got %d, want 1", len(tools))
	}

	toolSpec := tools[0]["toolSpecification"].(map[string]interface{})
	if toolSpec["name"] != "calculator" {
		t.Errorf("tool name: got %v, want calculator", toolSpec["name"])
	}
}

func TestBuildKiroPayload_ToolsFlatFormat(t *testing.T) {
	// Test Cursor-style flat format (no function field)
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
		Tools: []models.Tool{
			{
				Type:        "function",
				Name:        "my_tool",
				Description: "My custom tool",
				InputSchema: map[string]interface{}{"type": "object"},
			},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	// Tools should be in userInputMessageContext
	convState := payload["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInputMsg := currentMsg["userInputMessage"].(map[string]interface{})

	ctx := userInputMsg["userInputMessageContext"].(map[string]interface{})
	tools := ctx["tools"].([]map[string]interface{})
	toolSpec := tools[0]["toolSpecification"].(map[string]interface{})

	if toolSpec["name"] != "my_tool" {
		t.Errorf("tool name: got %v, want my_tool", toolSpec["name"])
	}
	if toolSpec["description"] != "My custom tool" {
		t.Errorf("tool description: got %v, want 'My custom tool'", toolSpec["description"])
	}
}

func TestBuildKiroPayload_ToolCalls(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "Calculate 2+2"},
			{
				Role:    "assistant",
				Content: "I'll calculate that",
				ToolCalls: []models.ToolCall{
					{
						ID:   "call_abc123",
						Type: "function",
						Function: &models.ToolCallFunction{
							Name:      "calculator",
							Arguments: `{"expression": "2+2"}`,
						},
					},
				},
			},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	// Verify conversationState exists
	convState, ok := payload["conversationState"].(map[string]interface{})
	if !ok {
		t.Fatal("conversationState not found in payload")
	}

	// Verify currentMessage exists
	currentMsg, ok := convState["currentMessage"].(map[string]interface{})
	if !ok {
		t.Fatal("currentMessage not found in conversationState")
	}

	// Verify user message content
	userInputMsg, ok := currentMsg["userInputMessage"].(map[string]interface{})
	if !ok {
		t.Fatal("userInputMessage not found")
	}
	// Content is now a string
	content, ok := userInputMsg["content"].(string)
	if !ok {
		t.Fatal("content is not a string")
	}
	if content != "Calculate 2+2" {
		t.Errorf("content: got %v, want 'Calculate 2+2'", content)
	}

	// Check history contains the assistant message with tool uses
	history, ok := convState["history"].([]map[string]interface{})
	if !ok {
		t.Fatal("history not found or wrong type")
	}

	foundToolUses := false
	for _, msg := range history {
		if assistantMsg, exists := msg["assistantResponseMessage"]; exists {
			if am, ok := assistantMsg.(map[string]interface{}); ok {
				if toolUses, exists := am["toolUses"]; exists {
					switch tu := toolUses.(type) {
					case []interface{}:
						if len(tu) > 0 {
							foundToolUses = true
							// Verify tool use structure
							if len(tu) > 0 {
								if tu0, ok := tu[0].(map[string]interface{}); ok {
									if tu0["toolUseId"] != "call_abc123" {
										t.Errorf("toolUseId: got %v, want call_abc123", tu0["toolUseId"])
									}
									if tu0["name"] != "calculator" {
										t.Errorf("tool name: got %v, want calculator", tu0["name"])
									}
								}
							}
						}
					case []map[string]interface{}:
						if len(tu) > 0 {
							foundToolUses = true
							if tu[0]["toolUseId"] != "call_abc123" {
								t.Errorf("toolUseId: got %v, want call_abc123", tu[0]["toolUseId"])
							}
							if tu[0]["name"] != "calculator" {
								t.Errorf("tool name: got %v, want calculator", tu[0]["name"])
							}
						}
					}
				}
			}
		}
		if foundToolUses {
			break
		}
	}
	if !foundToolUses {
		t.Error("Expected tool uses in history")
	}
}

func TestBuildKiroPayload_ToolResults(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "What is 2+2?"},
			{
				Role:    "assistant",
				Content: "Let me calculate that",
				ToolCalls: []models.ToolCall{
					{
						ID:   "call_abc123",
						Type: "function",
						Function: &models.ToolCallFunction{
							Name:      "calculator",
							Arguments: `{"expression": "2+2"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				Content:    "Result: 4",
				ToolCallID: "call_abc123",
			},
			{Role: "user", Content: "Thanks"},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	// Verify conversationState exists
	convState, ok := payload["conversationState"].(map[string]interface{})
	if !ok {
		t.Fatal("conversationState not found in payload")
	}

	// Verify user message is currentMessage
	currentMsg, ok := convState["currentMessage"].(map[string]interface{})
	if !ok {
		t.Fatal("currentMessage not found")
	}
	userInputMsg, ok := currentMsg["userInputMessage"].(map[string]interface{})
	if !ok {
		t.Fatal("userInputMessage not found")
	}
	// Content is now a string
	content, ok := userInputMsg["content"].(string)
	if !ok {
		t.Fatal("content is not a string")
	}
	if content != "Thanks" {
		t.Errorf("content: got %v, want 'Thanks'", content)
	}

	// Check history contains tool result
	history, ok := convState["history"].([]map[string]interface{})
	if !ok {
		t.Fatal("history not found or wrong type")
	}

	foundToolResult := false
	for _, msg := range history {
		if userMsg, exists := msg["userInputMessage"]; exists {
			if um, ok := userMsg.(map[string]interface{}); ok {
				if ctx, exists := um["userInputMessageContext"]; exists {
					if c, ok := ctx.(map[string]interface{}); ok {
						if toolResults, exists := c["toolResults"]; exists {
							switch tr := toolResults.(type) {
							case []interface{}:
								for _, trItem := range tr {
									if trm, ok := trItem.(map[string]interface{}); ok {
										if trm["toolUseId"] == "call_abc123" {
											foundToolResult = true
											// Verify tool result content
											if content, ok := trm["content"].([]interface{}); ok {
												if len(content) > 0 {
													if textBlock, ok := content[0].(map[string]interface{}); ok {
														if textBlock["text"] != "Result: 4" {
															t.Errorf("tool result text: got %v, want Result: 4", textBlock["text"])
														}
													}
												}
											}
											break
										}
									}
								}
							case []map[string]interface{}:
								for _, trItem := range tr {
									if trItem["toolUseId"] == "call_abc123" {
										foundToolResult = true
										// Verify tool result content
										if content, ok := trItem["content"].([]map[string]interface{}); ok {
											if len(content) > 0 {
												if content[0]["text"] != "Result: 4" {
													t.Errorf("tool result text: got %v, want Result: 4", content[0]["text"])
												}
											}
										}
										break
									}
								}
							}
						}
					}
				}
			}
		}
		if foundToolResult {
			break
		}
	}
	if !foundToolResult {
		t.Error("Expected tool result in history with toolUseId=call_abc123")
	}
}

func TestBuildKiroPayload_ProfileArn(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	// With profileArn
	payload, err := BuildKiroPayload(req, "test-conv-id", "arn:aws:codewhisperer:us-east-1:123456789:profile/my-profile")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}
	if payload["profileArn"] == "" {
		t.Error("profileArn should not be empty when provided")
	}

	// Without profileArn (empty) - key should not exist
	payload, err = BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}
	if _, exists := payload["profileArn"]; exists {
		t.Errorf("profileArn should not exist when not provided, got %v", payload["profileArn"])
	}
}

func TestBuildKiroPayload_MaxTokens(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model:     "claude-sonnet-4.5",
		Messages:  []models.ChatMessage{{Role: "user", Content: "Hello"}},
		MaxTokens: 4096,
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	if payload["maxTokens"] != 4096 {
		t.Errorf("maxTokens: got %v, want 4096", payload["maxTokens"])
	}
}

func TestBuildKiroPayload_Temperature(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model:       "claude-sonnet-4.5",
		Messages:    []models.ChatMessage{{Role: "user", Content: "Hello"}},
		Temperature: 0.7,
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	if payload["temperature"] != 0.7 {
		t.Errorf("temperature: got %v, want 0.7", payload["temperature"])
	}
}

func TestExtractTextContent(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{"string content", "Hello world", "Hello world"},
		{"nil content", nil, ""},
		{"empty string", "", ""},
		{"text block", []interface{}{
			map[string]interface{}{"type": "text", "text": "Hello"},
		}, "Hello"},
		{"multiple text blocks concatenated", []interface{}{
			map[string]interface{}{"type": "text", "text": "Hello"},
			map[string]interface{}{"type": "text", "text": "World"},
		}, "HelloWorld"}, // Note: no separator between blocks
		{"mixed blocks", []interface{}{
			map[string]interface{}{"type": "text", "text": "Text"},
			map[string]interface{}{"type": "image", "source": "data:image/png"},
		}, "Text"},
		{"unknown type", []interface{}{
			map[string]interface{}{"type": "unknown", "data": "value"},
		}, ""},
		{"integer fallback", 42, "42"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractTextContent(tc.input)
			if result != tc.expected {
				t.Errorf("extractTextContent(%v): got %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestExtractImages(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected int
	}{
		{"nil", nil, 0},
		{"string content", "hello", 0},
		{"empty array", []interface{}{}, 0},
		{"no images", []interface{}{
			map[string]interface{}{"type": "text", "text": "hello"},
		}, 0},
		{"one image url", []interface{}{
			map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
					"url": "https://example.com/image.png",
				},
			},
		}, 1},
		{"base64 image", []interface{}{
			map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
					"url": "data:image/png;base64,iVBORw0KGgo=",
				},
			},
		}, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractImages(tc.input)
			if len(result) != tc.expected {
				t.Errorf("extractImages: got %d images, want %d", len(result), tc.expected)
			}
		})
	}
}

func TestParseArguments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		key      string
		expected interface{}
	}{
		{"valid json", `{"key": "value"}`, "key", "value"},
		{"nested json", `{"a": {"b": 1}}`, "a", map[string]interface{}{"b": float64(1)}},
		{"empty string", "", "", map[string]interface{}{}},
		{"invalid json", `not json`, "", map[string]interface{}{}},
		{"array json", `["a", "b"]`, "", map[string]interface{}{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseArguments(tc.input)
			if tc.key == "" {
				// Just check it doesn't panic and returns a map
				if result == nil {
					t.Error("expected map, got nil")
				}
			} else {
				data, _ := json.Marshal(result)
				wantData, _ := json.Marshal(map[string]interface{}{tc.key: tc.expected})
				if string(data) != string(wantData) {
					t.Errorf("parseArguments(%q): got %s, want %s", tc.input, data, wantData)
				}
			}
		})
	}
}

func TestExtractContentBlocks(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected int
	}{
		{"nil", nil, 0},
		{"empty string", "", 0},
		{"non-empty string", "hello", 1},
		{"empty array", []interface{}{}, 0},
		{"text block", []interface{}{
			map[string]interface{}{"type": "text", "text": "Hello"},
		}, 1},
		{"empty text block", []interface{}{
			map[string]interface{}{"type": "text", "text": ""},
		}, 0},
		{"image url block", []interface{}{
			map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
					"url": "https://example.com/image.png",
				},
			},
		}, 1},
		{"base64 image block", []interface{}{
			map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
					"url": "data:image/png;base64,iVBORw0KGgo=",
				},
			},
		}, 1},
		{"mixed blocks", []interface{}{
			map[string]interface{}{"type": "text", "text": "Hello"},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "https://example.com/image.png"}},
		}, 2},
		{"unknown block type", []interface{}{
			map[string]interface{}{"type": "unknown", "data": "value"},
		}, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractContentBlocks(tc.input)
			if len(result) != tc.expected {
				t.Errorf("extractContentBlocks(%v): got %d blocks, want %d", tc.input, len(result), tc.expected)
			}
		})
	}
}

func TestExtractMediaType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"png", "data:image/png", "image/png"},
		{"png with base64", "data:image/png;base64,ABC123", "image/png"},
		{"jpeg", "data:image/jpeg", "image/png"},   // Falls back to png (no semicolon)
		{"gif", "data:image/gif", "image/png"},     // Falls back to png (no semicolon)
		{"webp", "data:image/webp", "image/png"},   // Falls back to png (no semicolon)
		{"svg", "data:image/svg+xml", "image/png"}, // Falls back to png (no semicolon)
		{"no semicolon", "not-a-data-url", "image/png"},
		{"empty semicolon", "data:", "image/png"},
		{"no prefix", "image/png", "image/png"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractMediaType(tc.input)
			if result != tc.expected {
				t.Errorf("extractMediaType(%q): got %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestExtractBase64Data(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"valid base64", "data:image/png;base64,ABC123", "ABC123"},
		{"jpeg base64", "data:image/jpeg;base64,xyz789", "xyz789"},
		{"no comma", "no-comma-here", ""},
		{"empty after comma", "data:image/png,", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractBase64Data(tc.input)
			if result != tc.expected {
				t.Errorf("extractBase64Data(%q): got %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestConvertTools_IgnoresNonFunction(t *testing.T) {
	tools := []models.Tool{
		{Type: "not-function"},
		{Type: "function", Function: &models.ToolFunction{Name: "valid"}},
	}

	result := convertTools(tools)
	if len(result) != 1 {
		t.Errorf("convertTools: got %d tools, want 1 (non-function type should be skipped)", len(result))
	}
}

func TestConvertTools_EmptyName(t *testing.T) {
	tools := []models.Tool{
		{Type: "function", Function: &models.ToolFunction{Name: ""}},
	}

	result := convertTools(tools)
	// Empty name should be skipped
	if len(result) != 0 {
		t.Errorf("convertTools: got %d tools, want 0", len(result))
	}
}

func TestNormalizeModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Already normalized
		{"claude-haiku-4.5", "claude-haiku-4.5"},
		{"claude-sonnet-4.5", "claude-sonnet-4.5"},
		{"claude-opus-4.5", "claude-opus-4.5"},

		// Dash to dot conversion
		{"claude-haiku-4-5", "claude-haiku-4.5"},
		{"claude-sonnet-4-5", "claude-sonnet-4.5"},
		{"claude-opus-4-5", "claude-opus-4.5"},
		{"claude-haiku-3-5", "claude-haiku-3.5"},
		{"claude-haiku-3-0", "claude-haiku-3.0"},
		{"claude-sonnet-4-0", "claude-sonnet-4.0"},

		// Date suffix removal
		{"claude-haiku-4-5-20251001", "claude-haiku-4.5"},
		{"claude-haiku-4-5-latest", "claude-haiku-4.5"},
		{"claude-sonnet-4-5-20250929", "claude-sonnet-4.5"},

		// Other models (pass through)
		{"auto", "auto"},
		{"deepseek-v3.2", "deepseek-v3.2"},
		{"qwen3-coder-next", "qwen3-coder-next"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeModelName(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeModelName(%q): got %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildKiroPayload_MultiTurnConversation(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "First question"},
			{Role: "assistant", Content: "First answer"},
			{Role: "user", Content: "Second question"},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	convState, ok := payload["conversationState"].(map[string]interface{})
	if !ok {
		t.Fatal("conversationState not found")
	}

	// currentMessage should have the last user message
	currentMsg, ok := convState["currentMessage"].(map[string]interface{})
	if !ok {
		t.Fatal("currentMessage not found")
	}
	userInputMsg, ok := currentMsg["userInputMessage"].(map[string]interface{})
	if !ok {
		t.Fatal("userInputMessage not found")
	}

	// Content is now a string
	content, ok := userInputMsg["content"].(string)
	if !ok {
		t.Fatal("content is not a string")
	}
	if content != "Second question" {
		t.Errorf("currentMessage content: got %v, want 'Second question'", content)
	}

	// History should have the previous messages
	history, ok := convState["history"].([]map[string]interface{})
	if !ok {
		t.Fatalf("history wrong type: %T", convState["history"])
	}

	if len(history) != 2 {
		t.Errorf("history length: got %d, want 2", len(history))
	}
}

func TestBuildKiroPayload_NoUserMessage(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "assistant", Content: "Hello, how can I help?"},
		},
	}

	_, err := BuildKiroPayload(req, "test-conv-id", "")
	if err == nil {
		t.Fatal("Expected error for request with no user message")
	}
}

func TestBuildKiroPayload_ToolCallsInHistory(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "Calculate 2+2"},
			{
				Role:    "assistant",
				Content: "I'll calculate that",
				ToolCalls: []models.ToolCall{
					{
						ID:   "call_abc123",
						Type: "function",
						Function: &models.ToolCallFunction{
							Name:      "calculator",
							Arguments: `{"expression": "2+2"}`,
						},
					},
				},
			},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	convState := payload["conversationState"].(map[string]interface{})
	history := convState["history"].([]map[string]interface{})

	// Find the assistant message with tool uses in history (Kiro format)
	var foundToolUse bool
	for _, msg := range history {
		if assistantMsg, ok := msg["assistantResponseMessage"]; ok {
			if am, ok := assistantMsg.(map[string]interface{}); ok {
				if toolUses, exists := am["toolUses"]; exists {
					switch tu := toolUses.(type) {
					case []interface{}:
						if len(tu) > 0 {
							foundToolUse = true
						}
					case []map[string]interface{}:
						if len(tu) > 0 {
							foundToolUse = true
						}
					}
				}
			}
		}
		if foundToolUse {
			break
		}
	}

	if !foundToolUse {
		t.Error("Expected to find tool uses in history")
	}
}

func TestBuildKiroPayload_WithImages(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "What's in this image?",
					},
					map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]interface{}{
							// Use data URL to avoid network fetch in test
							"url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAoAAAAKCAYAAACNMs+9AAAAFUlEQVR42mNk+M9QzwAEjDAGNzYQBxkHADPvBQcR6QmoAAAAAElFTkSuQmCC",
						},
					},
				},
			},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	convState := payload["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInputMsg := currentMsg["userInputMessage"].(map[string]interface{})

	// Content should include image data reference
	content, ok := userInputMsg["content"].(string)
	if !ok {
		t.Fatal("content is not a string")
	}
	// For data URLs, we add "[Image data included]" as reference
	if !strings.Contains(content, "Image data included") {
		t.Errorf("content should contain image data reference, got %q", content)
	}
	if !strings.Contains(content, "What's in this image?") {
		t.Errorf("content should contain original text, got %q", content)
	}

	// Images should be in the images field
	images, ok := userInputMsg["images"].([]map[string]interface{})
	if !ok {
		t.Fatal("images not found in userInputMessage")
	}
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
}

func TestBuildKiroPayload_TopP(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model:    "claude-sonnet-4.5",
		Messages: []models.ChatMessage{{Role: "user", Content: "Hello"}},
		TopP:     0.9,
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	if payload["topP"] != 0.9 {
		t.Errorf("topP: got %v, want 0.9", payload["topP"])
	}
}

func TestBuildKiroPayload_EnableStreaming(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model:    "claude-sonnet-4.5",
		Messages: []models.ChatMessage{{Role: "user", Content: "Hello"}},
		Stream:   true,
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	if payload["enableStreaming"] != true {
		t.Errorf("enableStreaming: got %v, want true", payload["enableStreaming"])
	}
}

func TestBuildKiroPayload_LastMessageTool(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "Calculate 2+2"},
			{
				Role:    "assistant",
				Content: "Let me calculate",
				ToolCalls: []models.ToolCall{
					{
						ID:   "call_abc",
						Type: "function",
						Function: &models.ToolCallFunction{
							Name:      "calculator",
							Arguments: `{"expression": "2+2"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				Content:    "4",
				ToolCallID: "call_abc",
			},
		},
	}

	// Last message is tool result - should return error
	_, err := BuildKiroPayload(req, "test-conv-id", "")
	if err == nil {
		t.Fatal("Expected error when last message is tool result")
	}
}

func TestBuildKiroPayload_SystemOnly(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "system", Content: "You are helpful."},
		},
	}

	_, err := BuildKiroPayload(req, "test-conv-id", "")
	if err == nil {
		t.Fatal("Expected error when only system message exists")
	}
}

func TestBuildKiroPayload_LastUserMessageNotAtEnd(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
			{Role: "assistant", Content: "How can I help?"}, // Assistant after user - weird but valid
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	convState := payload["conversationState"].(map[string]interface{})
	history := convState["history"].([]map[string]interface{})

	// History should have messages before and after the user message
	if len(history) != 2 {
		t.Errorf("history length: got %d, want 2", len(history))
	}
}

func TestNormalizeModelName_DateSuffix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Date suffix removal with single digit version
		{"claude-haiku-3-0-20251001", "claude-haiku-3.0"},
		{"claude-opus-4-0-20250915", "claude-opus-4.0"},
		// Latest suffix
		{"claude-sonnet-4-0-latest", "claude-sonnet-4.0"},
		{"claude-haiku-3-5-latest", "claude-haiku-3.5"},
		// Empty string
		{"", ""},
		// Single part (no dash)
		{"claude", "claude"},
		// Date with multi-digit month/day
		{"claude-haiku-4-5-20251231", "claude-haiku-4.5"},
		// Not a date (has letters) - the -4-5 gets converted to -4.5 first
		{"claude-haiku-4-5-abc", "claude-haiku-4.5-abc"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeModelName(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeModelName(%q): got %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildToolResults(t *testing.T) {
	// Test tool result with empty ToolCallID - uses Name as fallback
	msg := models.ChatMessage{
		Role:    "tool",
		Content: "tool output",
		Name:    "tool_name_fallback",
	}

	results := buildToolResults(msg)
	if len(results) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(results))
	}

	if results[0]["toolUseId"] != "tool_name_fallback" {
		t.Errorf("toolUseId: got %v, want tool_name_fallback", results[0]["toolUseId"])
	}
}

func TestBuildKiroPayload_OnlySystemWithUser(t *testing.T) {
	// Multiple system prompts followed by user
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "system", Content: "System 1"},
			{Role: "system", Content: "System 2"},
			{Role: "user", Content: "Hello"},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	// System prompts should be concatenated
	if payload["systemPrompt"] != "System 1\nSystem 2" {
		t.Errorf("systemPrompt: got %q", payload["systemPrompt"])
	}
}

func TestBuildKiroPayload_EmptyHistory(t *testing.T) {
	// Single user message - no history
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	convState := payload["conversationState"].(map[string]interface{})

	// History should be empty or not present
	if _, exists := convState["history"]; exists {
		history := convState["history"].([]map[string]interface{})
		if len(history) != 0 {
			t.Errorf("history should be empty for single message, got %d", len(history))
		}
	}
}

func TestBuildKiroPayload_WebSearchTool(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "Search for Go programming"},
		},
		Tools: []models.Tool{
			{
				Type: "function",
				Function: &models.ToolFunction{
					Name:        "web_search",
					Description: "Search the web for information",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"query": map[string]string{"type": "string"},
						},
					},
				},
			},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	convState := payload["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInputMsg := currentMsg["userInputMessage"].(map[string]interface{})

	ctx := userInputMsg["userInputMessageContext"].(map[string]interface{})
	tools := ctx["tools"].([]map[string]interface{})

	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	toolSpec := tools[0]["toolSpecification"].(map[string]interface{})
	if toolSpec["name"] != "web_search" {
		t.Errorf("tool name: got %v, want web_search", toolSpec["name"])
	}
}

func TestBuildKiroPayload_WebFetchTool(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "Fetch this URL"},
		},
		Tools: []models.Tool{
			{
				Type: "function",
				Function: &models.ToolFunction{
					Name:        "web_fetch",
					Description: "Fetch content from a URL",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"url": map[string]string{"type": "string"},
						},
					},
				},
			},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	convState := payload["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInputMsg := currentMsg["userInputMessage"].(map[string]interface{})

	ctx := userInputMsg["userInputMessageContext"].(map[string]interface{})
	tools := ctx["tools"].([]map[string]interface{})

	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	toolSpec := tools[0]["toolSpecification"].(map[string]interface{})
	if toolSpec["name"] != "web_fetch" {
		t.Errorf("tool name: got %v, want web_fetch", toolSpec["name"])
	}
}

func TestBuildKiroPayload_MultipleToolsIncludingWeb(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "Use tools to help me"},
		},
		Tools: []models.Tool{
			{
				Type: "function",
				Function: &models.ToolFunction{
					Name:        "web_search",
					Description: "Search the web",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"query": map[string]string{"type": "string"},
						},
					},
				},
			},
			{
				Type: "function",
				Function: &models.ToolFunction{
					Name:        "calculator",
					Description: "Perform calculations",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"expression": map[string]string{"type": "string"},
						},
					},
				},
			},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	convState := payload["conversationState"].(map[string]interface{})
	currentMsg := convState["currentMessage"].(map[string]interface{})
	userInputMsg := currentMsg["userInputMessage"].(map[string]interface{})

	ctx := userInputMsg["userInputMessageContext"].(map[string]interface{})
	tools := ctx["tools"].([]map[string]interface{})

	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	toolNames := make([]string, len(tools))
	for i, tool := range tools {
		toolSpec := tool["toolSpecification"].(map[string]interface{})
		toolNames[i] = toolSpec["name"].(string)
	}

	if !contains(toolNames, "web_search") {
		t.Error("web_search tool not found")
	}
	if !contains(toolNames, "calculator") {
		t.Error("calculator tool not found")
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
