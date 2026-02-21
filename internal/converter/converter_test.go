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

	// Check required fields
	if payload["conversationId"] != "test-conv-id" {
		t.Errorf("conversationId: got %v, want test-conv-id", payload["conversationId"])
	}
	if payload["modelId"] != "claude-sonnet-4.5" {
		t.Errorf("modelId: got %v, want claude-sonnet-4.5", payload["modelId"])
	}
	if payload["enableStreaming"] != false {
		t.Errorf("enableStreaming: got %v, want false", payload["enableStreaming"])
	}

	// Check messages converted
	messages := payload["messages"].([]map[string]interface{})
	if len(messages) != 2 {
		t.Errorf("messages count: got %d, want 2", len(messages))
	}
	if messages[0]["role"] != "user" {
		t.Errorf("first message role: got %v, want user", messages[0]["role"])
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

	messages := payload["messages"].([]map[string]interface{})
	msg := messages[0]

	toolCalls := msg["toolCalls"].([]map[string]interface{})
	if len(toolCalls) != 1 {
		t.Fatalf("toolCalls count: got %d, want 1", len(toolCalls))
	}

	if toolCalls[0]["id"] != "call_abc123" {
		t.Errorf("tool call id: got %v, want call_abc123", toolCalls[0]["id"])
	}
	if toolCalls[0]["name"] != "calculator" {
		t.Errorf("tool call name: got %v, want calculator", toolCalls[0]["name"])
	}

	// Check arguments parsed
	input := toolCalls[0]["input"].(map[string]interface{})
	if input["expression"] != "2+2" {
		t.Errorf("tool arguments: got %v, want 2+2", input["expression"])
	}
}

func TestBuildKiroPayload_ToolResults(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-sonnet-4.5",
		Messages: []models.ChatMessage{
			{
				Role:       "tool",
				Content:    "Result: 4",
				ToolCallID: "call_abc123",
			},
		},
	}

	payload, err := BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	messages := payload["messages"].([]map[string]interface{})
	msg := messages[0]

	// Tool role stays as "tool" (not converted to user)
	if msg["role"] != "tool" {
		t.Errorf("role: got %v, want tool", msg["role"])
	}

	// Check toolUseId
	if msg["toolUseId"] != "call_abc123" {
		t.Errorf("toolUseId: got %v, want call_abc123", msg["toolUseId"])
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

	// Without profileArn (empty)
	payload, err = BuildKiroPayload(req, "test-conv-id", "")
	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}
	if payload["profileArn"] != "" {
		t.Errorf("profileArn should be empty, got %v", payload["profileArn"])
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

func TestNormalizeModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-sonnet-4.5", "claude-sonnet-4.5"},
		{"claude-haiku-4-5", "claude-haiku-4.5"},
		{"claude-haiku-4-5-20251001", "claude-haiku-4.5"},
		{"claude-opus-3-0", "claude-opus-3.0"},
		{"claude-opus-3-0-20250630", "claude-opus-3.0"},
		{"auto", "auto"},
		{"deepseek-v3.2", "deepseek-v3.2"},
	}

	for _, tc := range tests {
		result := normalizeModelName(tc.input)
		if result != tc.expected {
			t.Errorf("normalizeModelName(%q): got %q, want %q", tc.input, result, tc.expected)
		}
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
