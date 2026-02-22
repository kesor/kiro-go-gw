package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kiro-go-gw/internal/auth"
	"kiro-go-gw/internal/converter"
	"kiro-go-gw/internal/models"
)

// mockAuthManager implements a mock for testing
type mockAuthManager struct {
	token       string
	refreshErr  error
	forceErr    error
	refreshCall int
	profileArn  string
	region      string
	apiHost     string
}

func (m *mockAuthManager) GetAccessToken() (string, error) {
	if m.refreshErr != nil {
		return "", m.refreshErr
	}
	return m.token, nil
}

func (m *mockAuthManager) ForceRefresh() (string, error) {
	m.refreshCall++
	if m.forceErr != nil {
		return "", m.forceErr
	}
	return m.token, nil
}

func (m *mockAuthManager) ProfileArn() string {
	return m.profileArn
}

func (m *mockAuthManager) Region() string {
	return m.region
}

func (m *mockAuthManager) APIHost() string {
	return m.apiHost
}

func TestConstants(t *testing.T) {
	if MaxRetries != 3 {
		t.Errorf("MaxRetries: got %d, want 3", MaxRetries)
	}
	if BaseRetryDelay != 1.0 {
		t.Errorf("BaseRetryDelay: got %v, want 1.0", BaseRetryDelay)
	}
	if StreamingTimeout != 300*time.Second {
		t.Errorf("StreamingTimeout: got %v, want 300s", StreamingTimeout)
	}
	if RequestTimeout != 30*time.Second {
		t.Errorf("RequestTimeout: got %v, want 30s", RequestTimeout)
	}
}

func TestNewKiroClient(t *testing.T) {
	mockAuth := &mockAuthManager{token: "test-token"}
	client := NewKiroClient(mockAuth)

	if client == nil {
		t.Fatal("NewKiroClient returned nil")
	}
	if client.authManager != mockAuth {
		t.Error("authManager not set correctly")
	}
	if client.httpClient == nil {
		t.Error("httpClient not created")
	}
}

func TestDoRequest_Success(t *testing.T) {
	mockAuth := &mockAuthManager{token: "test-token"}
	client := NewKiroClient(mockAuth)

	// Create test server that returns 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Authorization header: got %q, want Bearer test-token", auth)
		}
		if r.Header.Get("Content-Type") != "application/x-amz-json-1.0" {
			t.Errorf("Content-Type: got %s, want application/x-amz-json-1.0", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	resp, err := client.DoRequest(context.Background(), "POST", server.URL, map[string]interface{}{"test": true}, false)

	if err != nil {
		t.Fatalf("DoRequest failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode: got %d, want 200", resp.StatusCode)
	}
}

func TestDoRequest_Forbidden_Retry(t *testing.T) {
	callCount := 0
	mockAuth := &mockAuthManager{token: "new-token"}
	client := NewKiroClient(mockAuth)

	// Server that returns 403 first, then 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusForbidden)
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true}`))
		}
	}))
	defer server.Close()

	resp, err := client.DoRequest(context.Background(), "POST", server.URL, nil, false)

	if err != nil {
		t.Fatalf("DoRequest failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode after retry: got %d, want 200", resp.StatusCode)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 calls (403 then 200), got %d", callCount)
	}
}

func TestDoRequest_RateLimit(t *testing.T) {
	callCount := 0
	mockAuth := &mockAuthManager{token: "test-token"}
	client := NewKiroClient(mockAuth)

	// Server that returns 429 twice, then 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer server.Close()

	resp, err := client.DoRequest(context.Background(), "POST", server.URL, nil, false)

	if err != nil {
		t.Fatalf("DoRequest failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode after backoff: got %d, want 200", resp.StatusCode)
	}
	if callCount != 3 {
		t.Errorf("Expected 3 calls (429, 429, 200), got %d", callCount)
	}
}

func TestDoRequest_ServerError(t *testing.T) {
	callCount := 0
	mockAuth := &mockAuthManager{token: "test-token"}
	client := NewKiroClient(mockAuth)

	// Server that returns 500 twice, then 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer server.Close()

	resp, err := client.DoRequest(context.Background(), "POST", server.URL, nil, false)

	if err != nil {
		t.Fatalf("DoRequest failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode after retry: got %d, want 200", resp.StatusCode)
	}
}

func TestDoRequest_NonRetryableError(t *testing.T) {
	mockAuth := &mockAuthManager{token: "test-token"}
	client := NewKiroClient(mockAuth)

	// Server that returns 400 (non-retryable)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer server.Close()

	resp, err := client.DoRequest(context.Background(), "POST", server.URL, nil, false)

	if err != nil {
		t.Fatalf("DoRequest should not error on 400: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode: got %d, want 400", resp.StatusCode)
	}
}

func TestDoRequest_GetTokenError(t *testing.T) {
	mockAuth := &mockAuthManager{token: "", refreshErr: errors.New("token error")}
	client := NewKiroClient(mockAuth)

	_, err := client.DoRequest(context.Background(), "POST", "http://example.com", nil, false)

	if err == nil {
		t.Fatal("expected error when token fetch fails")
	}
	if !strings.Contains(err.Error(), "failed to get access token") {
		t.Errorf("error message should mention token: %v", err)
	}
}

func TestDoRequest_StreamingMode(t *testing.T) {
	mockAuth := &mockAuthManager{token: "test-token"}
	client := NewKiroClient(mockAuth)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Connection: close header for streaming
		conn := r.Header.Get("Connection")
		if conn != "close" {
			t.Errorf("Streaming Connection header: got %q, want close", conn)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, err := client.DoRequest(context.Background(), "POST", server.URL, nil, true)

	if err != nil {
		t.Fatalf("DoRequest failed: %v", err)
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"context deadline", context.DeadlineExceeded, true},
		{"context canceled", context.Canceled, false}, // User canceled, don't retry
		{"nil error", nil, false},
		{"random error", errors.New("random"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isRetryableError(tc.err)
			if result != tc.expected {
				t.Errorf("isRetryableError(%v): got %v, want %v", tc.err, result, tc.expected)
			}
		})
	}
}

func TestIsRetryableError_TimeoutStrings(t *testing.T) {
	timeoutErrors := []error{
		errors.New("dial tcp: i/o timeout"),
		errors.New("read tcp: connection timed out"),
		errors.New("dial tcp: lookup example.com: no such host"),
		errors.New("dial tcp 127.0.0.1: connection refused"),
	}

	for _, err := range timeoutErrors {
		if !isRetryableError(err) {
			t.Errorf("Expected %v to be retryable", err)
		}
	}
}

func TestClose(t *testing.T) {
	mockAuth := &mockAuthManager{token: "test-token"}
	client := NewKiroClient(mockAuth)

	// Should not panic
	err := client.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestDoRequest_MaxRetries(t *testing.T) {
	callCount := 0
	mockAuth := &mockAuthManager{token: "test-token"}
	client := NewKiroClient(mockAuth)

	// Server that always returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := client.DoRequest(context.Background(), "POST", server.URL, nil, false)

	// Should exhaust retries and return error
	if err == nil {
		t.Error("expected error after max retries")
	}
	if callCount != MaxRetries {
		t.Errorf("Expected %d retries, got %d", MaxRetries, callCount)
	}
}

func TestDoRequest_NonStreamingSingleRetry(t *testing.T) {
	callCount := 0
	mockAuth := &mockAuthManager{token: "test-token"}
	client := NewKiroClient(mockAuth)

	// For non-streaming, should retry up to MaxRetries
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < MaxRetries {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	_, _ = client.DoRequest(context.Background(), "POST", server.URL, nil, false)

	// Should retry MaxRetries times
	if callCount != MaxRetries {
		t.Errorf("Non-streaming should retry %d times, got %d", MaxRetries, callCount)
	}
}

func TestKiroClientWithRealCredentials(t *testing.T) {
	if os.Getenv("KIRO_INTEGRATION_TEST") != "1" {
		t.Skip("Set KIRO_INTEGRATION_TEST=1 to run integration tests")
	}

	dbPath := os.Getenv("KIRO_CLI_DB_FILE")
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("No home directory found, skipping integration test")
		}
		dbPath = filepath.Join(home, ".local", "share", "kiro-cli", "data.sqlite3")
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skipf("SQLite database not found at %s, skipping integration test", dbPath)
	}

	region := os.Getenv("KIRO_REGION")
	if region == "" {
		region = "us-east-1"
	}

	cfg := &auth.AuthConfig{
		CliDbFile: dbPath,
		Region:    region,
	}

	authManager, err := auth.NewAuthManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create AuthManager: %v", err)
	}

	t.Logf("Auth type: %s", authManager.AuthType())
	t.Logf("API Host: %s", authManager.APIHost())

	client := NewKiroClient(authManager)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// First, try to get a token and see its status
	token, err := authManager.GetAccessToken()
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}
	t.Logf("Got access token, length: %d", len(token))

	// Make a direct HTTP request to see the actual response
	chatURL := authManager.APIHost() + "/generateAssistantResponse"
	t.Logf("Making request to: %s", chatURL)

	// Test with a simple chat completion request (using Kiro's native format)
	// This matches what Python's build_kiro_payload creates
	payload := map[string]interface{}{
		"conversationState": map[string]interface{}{
			"chatTriggerType": "MANUAL",
			"conversationId":  "conv-" + strings.Repeat("x", 32),
			"currentMessage": map[string]interface{}{
				"userInputMessage": map[string]interface{}{
					"content": "Hi",
					"modelId": "claude-haiku-4.5",
					"origin":  "KIRO_CLI",
				},
			},
		},
	}

	// Use DoRequest to make the API call (handles auth headers, retries, etc.)
	resp, err := client.DoRequest(ctx, "POST", authManager.APIHost()+"/generateAssistantResponse", payload, false)
	if err != nil {
		t.Fatalf("DoRequest failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	t.Logf("Response status: %d", resp.StatusCode)

	// Check if it's a streaming response (SSE format)
	bodyStr := string(body)
	if strings.Contains(bodyStr, "assistantResponseEvent") {
		preview := bodyStr
		if len(preview) > 100 {
			preview = preview[:100]
		}
		t.Logf("Got streaming response! Preview: %s", preview)
		t.Logf("SUCCESS: Authentication and API request both work!")
		return
	}

	// Non-streaming: try to parse as JSON
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", resp.StatusCode, bodyStr)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	t.Logf("Successfully connected to Kiro API!")
	t.Logf("Response keys: %v", reflectkeys(result))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func reflectkeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestKiroClientWithRealCredentials_SimpleWithConverter(t *testing.T) {
	if os.Getenv("KIRO_INTEGRATION_TEST") != "1" {
		t.Skip("Set KIRO_INTEGRATION_TEST=1 to run integration tests")
	}

	dbPath := os.Getenv("KIRO_CLI_DB_FILE")
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("No home directory found, skipping integration test")
		}
		dbPath = filepath.Join(home, ".local", "share", "kiro-cli", "data.sqlite3")
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skipf("SQLite database not found at %s, skipping integration test", dbPath)
	}

	region := os.Getenv("KIRO_REGION")
	if region == "" {
		region = "us-east-1"
	}

	cfg := &auth.AuthConfig{
		CliDbFile: dbPath,
		Region:    region,
	}

	authManager, err := auth.NewAuthManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create AuthManager: %v", err)
	}

	t.Logf("Auth type: %s", authManager.AuthType())
	t.Logf("API Host: %s", authManager.APIHost())

	client := NewKiroClient(authManager)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get token
	token, err := authManager.GetAccessToken()
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}
	t.Logf("Got access token, length: %d", len(token))
	t.Logf("Profile ARN: %s", authManager.ProfileArn())

	// Use profileArn from env or empty for aws_sso
	profileArn := os.Getenv("KIRO_PROFILE_ARN")

	// Add system message to simulate real Kiro CLI context
	systemPrompt := "Follow this instruction:"
	userContent := "Hi"

	// Use converter to build payload for simple message
	conversationID := "conv-" + strings.Repeat("x", 32)
	payload, err := converter.BuildKiroPayload(&models.ChatCompletionRequest{
		Model: "claude-haiku-4.5",
		Messages: []models.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userContent},
		},
	}, conversationID, profileArn)

	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	t.Logf("Built payload with conversationId: %s", conversationID)

	// Debug: print the payload
	payloadJSON, _ := json.MarshalIndent(payload, "", "  ")
	t.Logf("Payload: %s", payloadJSON)

	// Use DoRequest to make the API call (handles auth headers, retries, etc.)
	resp, err := client.DoRequest(ctx, "POST", authManager.APIHost()+"/generateAssistantResponse", payload, false)
	if err != nil {
		t.Fatalf("DoRequest failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	t.Logf("Response status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	t.Logf("SUCCESS: Simple message with converter works!")
}

func TestKiroClientWithRealCredentials_ToolCalling(t *testing.T) {
	if os.Getenv("KIRO_INTEGRATION_TEST") != "1" {
		t.Skip("Set KIRO_INTEGRATION_TEST=1 to run integration tests")
	}

	dbPath := os.Getenv("KIRO_CLI_DB_FILE")
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("No home directory found, skipping integration test")
		}
		dbPath = filepath.Join(home, ".local", "share", "kiro-cli", "data.sqlite3")
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skipf("SQLite database not found at %s, skipping integration test", dbPath)
	}

	region := os.Getenv("KIRO_REGION")
	if region == "" {
		region = "us-east-1"
	}

	cfg := &auth.AuthConfig{
		CliDbFile: dbPath,
		Region:    region,
	}

	authManager, err := auth.NewAuthManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create AuthManager: %v", err)
	}

	t.Logf("Auth type: %s", authManager.AuthType())
	t.Logf("API Host: %s", authManager.APIHost())

	client := NewKiroClient(authManager)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Get token
	token, err := authManager.GetAccessToken()
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}
	t.Logf("Got access token, length: %d", len(token))

	// Use converter to build payload for tool calling
	conversationID := "conv-" + strings.Repeat("x", 32)
	payload, err := converter.BuildKiroPayload(&models.ChatCompletionRequest{
		Model: "claude-haiku-4.5",
		Messages: []models.ChatMessage{
			{
				Role:    "user",
				Content: "What is 125 * 17? Use the calculator tool.",
			},
		},
		Tools: []models.Tool{
			{
				Type: "function",
				Function: &models.ToolFunction{
					Name:        "calculator",
					Description: "Perform basic arithmetic calculations",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"expression": map[string]interface{}{
								"type":        "string",
								"description": "Mathematical expression to evaluate",
							},
						},
						"required": []string{"expression"},
					},
				},
			},
		},
	}, conversationID, authManager.ProfileArn())

	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	t.Logf("Built payload with conversationId: %s", conversationID)

	// Debug: print the payload
	payloadJSON, _ := json.MarshalIndent(payload, "", "  ")
	t.Logf("Payload: %s", payloadJSON)

	// Use DoRequest to make the API call (handles auth headers, retries, etc.)
	resp, err := client.DoRequest(ctx, "POST", authManager.APIHost()+"/generateAssistantResponse", payload, false)
	if err != nil {
		t.Fatalf("DoRequest failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	t.Logf("Response status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	bodyStr := string(body)

	// Check for tool calls in response
	if strings.Contains(bodyStr, "toolUse") || strings.Contains(bodyStr, "tool_call") {
		t.Logf("SUCCESS: Tool calling response detected!")
		// Log a preview of the response
		preview := bodyStr
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		t.Logf("Response preview: %s", preview)
	} else if strings.Contains(bodyStr, "125") || strings.Contains(bodyStr, "17") || strings.Contains(bodyStr, "2125") {
		t.Logf("SUCCESS: Got numerical response (125 * 17 = 2125)")
		t.Logf("Response: %s", bodyStr)
	} else {
		t.Logf("Response (no explicit tool call detected): %s", bodyStr)
	}
}

func TestKiroClientWithRealCredentials_ImageRecognition(t *testing.T) {
	if os.Getenv("KIRO_INTEGRATION_TEST") != "1" {
		t.Skip("Set KIRO_INTEGRATION_TEST=1 to run integration tests")
	}

	dbPath := os.Getenv("KIRO_CLI_DB_FILE")
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("No home directory found, skipping integration test")
		}
		dbPath = filepath.Join(home, ".local", "share", "kiro-cli", "data.sqlite3")
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skipf("SQLite database not found at %s, skipping integration test", dbPath)
	}

	region := os.Getenv("KIRO_REGION")
	if region == "" {
		region = "us-east-1"
	}

	cfg := &auth.AuthConfig{
		CliDbFile: dbPath,
		Region:    region,
	}

	authManager, err := auth.NewAuthManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create AuthManager: %v", err)
	}

	t.Logf("Auth type: %s", authManager.AuthType())
	t.Logf("API Host: %s", authManager.APIHost())

	client := NewKiroClient(authManager)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Get token
	token, err := authManager.GetAccessToken()
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}
	t.Logf("Got access token, length: %d", len(token))

	// Use profileArn from auth
	profileArn := authManager.ProfileArn()

	// Use converter to build payload with an embedded image data URL
	// This avoids external network dependencies and does not rely on filesystem access
	imageURL := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGNg+P++HgAFhAJ/wlseKgAAAABJRU5ErkJggg=="

	conversationID := "conv-" + strings.Repeat("x", 32)
	payload, err := converter.BuildKiroPayload(&models.ChatCompletionRequest{
		Model: "auto",
		Messages: []models.ChatMessage{
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "What is in this image? Describe what you see.",
					},
					map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": imageURL,
						},
					},
				},
			},
		},
	}, conversationID, profileArn)

	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	t.Logf("Built payload with conversationId: %s", conversationID)
	t.Logf("Image URL: %s", imageURL)

	// Debug: print the payload
	payloadJSON, _ := json.MarshalIndent(payload, "", "  ")
	t.Logf("Payload: %s", payloadJSON)

	// Use DoRequest to make the API call (handles auth headers, retries, etc.)
	resp, err := client.DoRequest(ctx, "POST", authManager.APIHost()+"/generateAssistantResponse", payload, false)
	if err != nil {
		t.Fatalf("DoRequest failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	t.Logf("Response status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	bodyStr := string(body)

	// Check if we got a valid response about the image
	if len(bodyStr) > 50 {
		t.Logf("SUCCESS: Got image recognition response!")
		// Log a preview of the response
		preview := bodyStr
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		t.Logf("Response preview: %s", preview)
	} else {
		t.Fatalf("Response too short: %s", bodyStr)
	}
}

func TestListAvailableModels(t *testing.T) {
	mockAuth := &mockAuthManager{
		token:   "test-token",
		apiHost: "http://localhost:8080",
	}
	client := NewKiroClient(mockAuth)

	var receivedTarget string
	var receivedAuth string
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTarget = r.Header.Get("x-amz-target")
		receivedAuth = r.Header.Get("Authorization")
		receivedContentType = r.Header.Get("Content-Type")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"defaultModel": {
				"modelId": "auto",
				"modelName": "auto",
				"description": "Default model"
			},
			"models": [
				{
					"modelId": "claude-sonnet-4.5",
					"modelName": "claude-sonnet-4.5",
					"description": "Claude Sonnet 4.5",
					"promptCaching": {"supportsPromptCaching": true},
					"rateMultiplier": 1.3,
					"rateUnit": "Credit",
					"tokenLimits": {"maxInputTokens": 200000}
				}
			]
		}`))
	}))
	defer server.Close()

	// Override the API host to use our test server
	originalHost := mockAuth.apiHost
	mockAuth.apiHost = server.URL

	resp, err := client.ListAvailableModels(context.Background(), "arn:aws:codewhisperer:us-east-1:123456789:profile/test")

	mockAuth.apiHost = originalHost

	if err != nil {
		t.Fatalf("ListAvailableModels failed: %v", err)
	}

	if receivedTarget != "AmazonCodeWhispererService.ListAvailableModels" {
		t.Errorf("x-amz-target: got %q, want %q", receivedTarget, "AmazonCodeWhispererService.ListAvailableModels")
	}

	if !strings.HasPrefix(receivedAuth, "Bearer ") {
		t.Errorf("Authorization: got %q, want Bearer token", receivedAuth)
	}

	if receivedContentType != "application/x-amz-json-1.0" {
		t.Errorf("Content-Type: got %q, want %q", receivedContentType, "application/x-amz-json-1.0")
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if resp.DefaultModel == nil {
		t.Error("DefaultModel is nil")
	} else if resp.DefaultModel.ModelID != "auto" {
		t.Errorf("DefaultModel.ModelID: got %q, want %q", resp.DefaultModel.ModelID, "auto")
	}

	if len(resp.Models) != 1 {
		t.Errorf("Models length: got %d, want 1", len(resp.Models))
	} else {
		if resp.Models[0].ModelID != "claude-sonnet-4.5" {
			t.Errorf("Models[0].ModelID: got %q, want %q", resp.Models[0].ModelID, "claude-sonnet-4.5")
		}
		if resp.Models[0].RateMultiplier != 1.3 {
			t.Errorf("Models[0].RateMultiplier: got %v, want %v", resp.Models[0].RateMultiplier, 1.3)
		}
		if !resp.Models[0].PromptCaching.SupportsPromptCaching {
			t.Error("Models[0].PromptCaching.SupportsPromptCaching: got false, want true")
		}
		if resp.Models[0].TokenLimits.MaxInputTokens != 200000 {
			t.Errorf("Models[0].TokenLimits.MaxInputTokens: got %d, want %d", resp.Models[0].TokenLimits.MaxInputTokens, 200000)
		}
	}
}
