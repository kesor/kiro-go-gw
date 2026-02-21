package streaming

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestParseAndConvert_ContentDelta(t *testing.T) {
	handler := NewStreamHandler("claude-sonnet-4.5")

	line := `data: {"eventType":"contentBlockDelta","content":"Hello"}`
	output, err := handler.ParseAndConvert(line)

	if err != nil {
		t.Fatalf("ParseAndConvert failed: %v", err)
	}
	if output == "" {
		t.Fatal("expected output, got empty")
	}

	// Should contain OpenAI chunk format
	if !strings.Contains(output, `"content":"Hello"`) {
		t.Errorf("expected content 'Hello' in output: %s", output)
	}
	if !strings.Contains(output, `"role":"assistant"`) {
		t.Errorf("expected role 'assistant' in first chunk: %s", output)
	}
}

func TestParseAndConvert_ContentDeltaWithoutRole(t *testing.T) {
	handler := NewStreamHandler("claude-sonnet-4.5")

	// First chunk adds role
	handler.ParseAndConvert(`data: {"eventType":"contentBlockDelta","content":"Hello"}`)
	// Second chunk should not add role
	output, _ := handler.ParseAndConvert(`data: {"eventType":"contentBlockDelta","content":" World"}`)

	if strings.Contains(output, `"role"`) {
		t.Errorf("second chunk should not include role: %s", output)
	}
}

func TestParseAndConvert_ToolUse(t *testing.T) {
	handler := NewStreamHandler("claude-sonnet-4.5")

	line := `data: {"eventType":"toolUse","toolCallId":"call_abc123","toolName":"calculator"}`
	output, err := handler.ParseAndConvert(line)

	if err != nil {
		t.Fatalf("ParseAndConvert failed: %v", err)
	}
	if output == "" {
		t.Fatal("expected output, got empty")
	}

	if !strings.Contains(output, `"id":"call_abc123"`) {
		t.Errorf("expected tool call id: %s", output)
	}
	if !strings.Contains(output, `"name":"calculator"`) {
		t.Errorf("expected tool name: %s", output)
	}
}

func TestParseAndConvert_Done(t *testing.T) {
	handler := NewStreamHandler("claude-sonnet-4.5")

	_, err := handler.ParseAndConvert("[DONE]")

	if err != io.EOF {
		t.Errorf("expected EOF for [DONE], got: %v", err)
	}
}

func TestParseAndConvert_InvalidJSON(t *testing.T) {
	handler := NewStreamHandler("claude-sonnet-4.5")

	output, err := handler.ParseAndConvert("not valid json at all")

	// Should return nil error (skip non-JSON lines)
	if err != nil {
		t.Errorf("expected nil error for invalid JSON, got: %v", err)
	}
	if output != "" {
		t.Errorf("expected empty output for invalid JSON, got: %s", output)
	}
}

func TestParseAndConvert_EmptyLines(t *testing.T) {
	handler := NewStreamHandler("claude-sonnet-4.5")

	tests := []string{
		"",
		"   ",
		"\t",
	}

	for _, line := range tests {
		output, err := handler.ParseAndConvert(line)
		if err != nil {
			t.Errorf("empty line should not error, got: %v", err)
		}
		if output != "" {
			t.Errorf("empty line should produce empty output, got: %s", output)
		}
	}
}

func TestParseAndConvert_Comment(t *testing.T) {
	handler := NewStreamHandler("claude-sonnet-4.5")

	line := `# This is a comment`
	output, err := handler.ParseAndConvert(line)

	if err != nil {
		t.Errorf("comment should not error, got: %v", err)
	}
	if output != "" {
		t.Errorf("comment should produce empty output, got: %s", output)
	}
}

func TestParseAndConvert_DataPrefix(t *testing.T) {
	handler := NewStreamHandler("claude-sonnet-4.5")

	// Without data: prefix
	line1 := `{"eventType":"contentBlockDelta","content":"Hello"}`
	output1, _ := handler.ParseAndConvert(line1)

	// With data: prefix
	line2 := `data: {"eventType":"contentBlockDelta","content":"Hello"}`
	output2, _ := handler.ParseAndConvert(line2)

	// Both should produce similar output
	if output1 == "" || output2 == "" {
		t.Error("both formats should produce output")
	}
}

func TestParseAndConvert_UnknownEvent(t *testing.T) {
	handler := NewStreamHandler("claude-sonnet-4.5")

	line := `data: {"eventType":"unknownEvent","foo":"bar"}`
	output, err := handler.ParseAndConvert(line)

	// Unknown events should be silently skipped
	if err != nil {
		t.Errorf("unknown event should not error, got: %v", err)
	}
	if output != "" {
		t.Errorf("unknown event should produce no output, got: %s", output)
	}
}

func TestStreamToOpenAI(t *testing.T) {
	input := `data: {"eventType":"contentBlockDelta","content":"Hello"}
data: {"eventType":"contentBlockDelta","content":" World"}
data: {"eventType":"messageStop"}
data: [DONE]
`

	reader := bytes.NewBufferString(input)
	ch, err := StreamToOpenAI(reader, "claude-sonnet-4.5")

	if err != nil {
		t.Fatalf("StreamToOpenAI failed: %v", err)
	}

	var outputs []string
	for data := range ch {
		outputs = append(outputs, data)
	}

	// Should have outputs (2 content deltas + messageStop + DONE)
	if len(outputs) < 2 {
		t.Errorf("expected at least 2 outputs, got %d", len(outputs))
	}

	// Check first chunk has role
	if !strings.Contains(outputs[0], `"role":"assistant"`) {
		t.Error("first chunk should have role")
	}

	// Check last is DONE
	lastOutput := outputs[len(outputs)-1]
	if !strings.Contains(lastOutput, "[DONE]") {
		t.Errorf("last output should contain DONE: %s", lastOutput)
	}
}

func TestStreamHandler_CompletionID(t *testing.T) {
	handler := NewStreamHandler("test-model")

	id1 := handler.CompletionID()
	id2 := handler.CompletionID()

	// Same handler should return same ID
	if id1 != id2 {
		t.Errorf("completion ID should be stable: %s != %s", id1, id2)
	}

	// Should have expected prefix
	if !strings.HasPrefix(id1, "chatcmpl-") {
		t.Errorf("completion ID should have chatcmpl- prefix: %s", id1)
	}
}

func TestNewStreamHandler(t *testing.T) {
	handler := NewStreamHandler("test-model")

	if handler.completionID == "" {
		t.Error("completionID should not be empty")
	}
	if handler.created == 0 {
		t.Error("created timestamp should not be zero")
	}
	if handler.model != "test-model" {
		t.Errorf("model: got %s, want test-model", handler.model)
	}
	if !handler.firstChunk {
		t.Error("firstChunk should be true initially")
	}
}

func TestParseStream(t *testing.T) {
	input := "line1\nline2\nline3\n"
	reader := bytes.NewBufferString(input)

	ch := ParseStream(reader)

	var lines []string
	for line := range ch {
		lines = append(lines, line)
	}

	if len(lines) != 3 {
		t.Errorf("ParseStream: got %d lines, want 3", len(lines))
	}
	if lines[0] != "line1" {
		t.Errorf("first line: got %q, want line1", lines[0])
	}
}
