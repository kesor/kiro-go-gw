package converter

import (
	"encoding/json"
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
	if userInputMsg["origin"] != "AI_EDITOR" {
		t.Errorf("origin: got %v, want AI_EDITOR", userInputMsg["origin"])
	}
	// Content is now an array of content blocks
	contentBlocks, ok := userInputMsg["content"].([]map[string]interface{})
	if !ok {
		t.Fatal("content is not an array of content blocks")
	}
	if len(contentBlocks) != 1 || contentBlocks[0]["text"] != "Hello" {
		t.Errorf("content: got %v, want [{text: Hello}]", contentBlocks)
	}

	// Check history contains previous messages (all messages for now)
	history, ok := convState["history"].([]map[string]interface{})
	if !ok {
		t.Logf("history: not found or wrong type")
	} else if len(history) > 0 {
		t.Logf("history has %d messages", len(history))
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

	tools := payload["toolDefinitions"]
	if tools == nil {
		t.Fatal("toolDefinitions is nil")
	}

	toolList := tools.([]map[string]interface{})
	if len(toolList) != 1 {
		t.Errorf("tool count: got %d, want 1", len(toolList))
	}

	toolSpec := toolList[0]["toolSpec"].(map[string]interface{})
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

	tools := payload["toolDefinitions"].([]map[string]interface{})
	toolSpec := tools[0]["toolSpec"].(map[string]interface{})

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
	// Content is now an array of content blocks
	contentBlocks, ok := userInputMsg["content"].([]map[string]interface{})
	if !ok {
		t.Fatal("content is not an array of content blocks")
	}
	if len(contentBlocks) != 1 || contentBlocks[0]["text"] != "Calculate 2+2" {
		t.Errorf("content: got %v, want [{text: Calculate 2+2}]", contentBlocks)
	}

	// Check history contains the assistant message with tool calls
	history, ok := convState["history"].([]map[string]interface{})
	if !ok {
		t.Fatal("history not found or wrong type")
	}

	foundToolCalls := false
	for _, msg := range history {
		if toolCalls, exists := msg["toolCalls"]; exists {
			// Handle both []interface{} and []map[string]interface{} types
			switch tc := toolCalls.(type) {
			case []interface{}:
				if len(tc) > 0 {
					foundToolCalls = true
				}
			case []map[string]interface{}:
				if len(tc) > 0 {
					foundToolCalls = true
				}
			}
			if foundToolCalls {
				break
			}
		}
	}
	if !foundToolCalls {
		t.Error("Expected tool calls in history")
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
	// Content is now an array of content blocks
	contentBlocks, ok := userInputMsg["content"].([]map[string]interface{})
	if !ok {
		t.Fatal("content is not an array of content blocks")
	}
	if len(contentBlocks) != 1 || contentBlocks[0]["text"] != "Thanks" {
		t.Errorf("content: got %v, want [{text: Thanks}]", contentBlocks)
	}

	// Check history contains tool result
	history, ok := convState["history"].([]map[string]interface{})
	if !ok {
		t.Fatal("history not found or wrong type")
	}

	foundToolResult := false
	for _, msg := range history {
		role, _ := msg["role"].(string)
		if role == "tool" {
			toolUseID, _ := msg["toolUseId"].(string)
			if toolUseID == "call_abc123" {
				foundToolResult = true
				break
			}
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
		{"multiple text blocks", []interface{}{
			map[string]interface{}{"type": "text", "text": "Hello "},
			map[string]interface{}{"type": "text", "text": "World"},
		}, "Hello World"},
		{"mixed blocks", []interface{}{
			map[string]interface{}{"type": "text", "text": "Text"},
			map[string]interface{}{"type": "image", "source": "data:image/png"},
		}, "Text"},
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

	// Content is now an array of content blocks
	contentBlocks, ok := userInputMsg["content"].([]map[string]interface{})
	if !ok {
		t.Fatal("content is not an array of content blocks")
	}
	if len(contentBlocks) != 1 || contentBlocks[0]["text"] != "Second question" {
		t.Errorf("currentMessage content: got %v, want [{text: Second question}]", contentBlocks)
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

	// Find the assistant message with tool calls in history
	var foundToolCall bool
	for _, msg := range history {
		if toolCalls, ok := msg["toolCalls"]; ok {
			// Handle both []interface{} and []map[string]interface{} types
			switch tc := toolCalls.(type) {
			case []interface{}:
				if len(tc) > 0 {
					foundToolCall = true
				}
			case []map[string]interface{}:
				if len(tc) > 0 {
					foundToolCall = true
				}
			}
			if foundToolCall {
				break
			}
		}
	}

	if !foundToolCall {
		t.Error("Expected to find tool calls in history")
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
							"url": "https://example.com/image.png",
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

	contentBlocks, ok := userInputMsg["content"].([]map[string]interface{})
	if !ok {
		t.Fatal("content is not an array of content blocks")
	}

	if len(contentBlocks) != 2 {
		t.Fatalf("expected 2 content blocks (text + image), got %d", len(contentBlocks))
	}

	textBlock := contentBlocks[0]
	if textBlock["type"] != "text" || textBlock["text"] != "What's in this image?" {
		t.Errorf("text block: got %v", textBlock)
	}

	imageBlock := contentBlocks[1]
	if imageBlock["type"] != "image" {
		t.Errorf("image block type: got %v", imageBlock["type"])
	}
	source, ok := imageBlock["source"].(map[string]interface{})
	if !ok {
		t.Fatal("image source not found")
	}
	if source["type"] != "url" {
		t.Errorf("image source type: got %v", source["type"])
	}
	if source["url"] != "https://example.com/image.png" {
		t.Errorf("image url: got %v", source["url"])
	}
}
