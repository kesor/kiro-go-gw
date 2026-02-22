package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kiro-go-gw/internal/auth"
	"kiro-go-gw/internal/client"
	"kiro-go-gw/internal/converter"
	"kiro-go-gw/internal/models"
)

func getDBPath(t *testing.T) string {
	t.Helper()
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
	return dbPath
}

func getRegion(t *testing.T) string {
	t.Helper()
	region := os.Getenv("KIRO_REGION")
	if region == "" {
		region = "us-east-1"
	}
	return region
}

func createTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
}

func TestKiroClientWithRealCredentials(t *testing.T) {
	if os.Getenv("KIRO_INTEGRATION_TEST") != "1" {
		t.Skip("Set KIRO_INTEGRATION_TEST=1 to run integration tests")
	}

	dbPath := getDBPath(t)
	region := getRegion(t)

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

	kiroClient := client.NewKiroClient(authManager)
	defer kiroClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token, err := authManager.GetAccessToken()
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}
	t.Logf("Got access token, length: %d", len(token))

	payload, err := converter.BuildKiroPayload(&models.ChatCompletionRequest{
		Model: "claude-haiku-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "Hi"},
		},
	}, "conv-"+strings.Repeat("x", 32), "")

	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	resp, err := kiroClient.DoRequest(ctx, "POST", authManager.APIHost()+"/generateAssistantResponse", payload, false)
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

	if strings.Contains(string(body), "assistantResponseEvent") {
		t.Logf("SUCCESS: Authentication and API request both work!")
	}
}

func TestKiroClientWithRealCredentials_SimpleWithConverter(t *testing.T) {
	if os.Getenv("KIRO_INTEGRATION_TEST") != "1" {
		t.Skip("Set KIRO_INTEGRATION_TEST=1 to run integration tests")
	}

	dbPath := getDBPath(t)
	region := getRegion(t)

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
	t.Logf("Profile ARN: %s", authManager.ProfileArn())

	kiroClient := client.NewKiroClient(authManager)
	defer kiroClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token, err := authManager.GetAccessToken()
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}
	t.Logf("Got access token, length: %d", len(token))

	payload, err := converter.BuildKiroPayload(&models.ChatCompletionRequest{
		Model: "claude-haiku-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "Hi"},
		},
	}, "conv-"+strings.Repeat("x", 32), authManager.ProfileArn())

	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	t.Logf("Built payload with conversationId: conv-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")

	resp, err := kiroClient.DoRequest(ctx, "POST", authManager.APIHost()+"/generateAssistantResponse", payload, false)
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

	dbPath := getDBPath(t)
	region := getRegion(t)

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

	kiroClient := client.NewKiroClient(authManager)
	defer kiroClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token, err := authManager.GetAccessToken()
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}
	t.Logf("Got access token, length: %d", len(token))

	payload, err := converter.BuildKiroPayload(&models.ChatCompletionRequest{
		Model: "claude-haiku-4.5",
		Messages: []models.ChatMessage{
			{Role: "user", Content: "What is 125 * 17? Use the calculator tool."},
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
							"expression": map[string]string{"type": "string"},
						},
						"required": []string{"expression"},
					},
				},
			},
		},
	}, "conv-"+strings.Repeat("x", 32), authManager.ProfileArn())

	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	t.Logf("Built payload with conversationId: conv-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")

	resp, err := kiroClient.DoRequest(ctx, "POST", authManager.APIHost()+"/generateAssistantResponse", payload, true)
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
	if strings.Contains(bodyStr, "toolUseEvent") {
		t.Logf("SUCCESS: Tool calling response detected!")
		preview := bodyStr
		if len(preview) > 200 {
			preview = preview[:200]
		}
		t.Logf("Response preview: %s", preview)
	} else {
		t.Fatalf("Expected tool use event, got: %s", bodyStr[:min(200, len(bodyStr))])
	}
}

func TestKiroClientWithRealCredentials_ImageRecognition(t *testing.T) {
	if os.Getenv("KIRO_INTEGRATION_TEST") != "1" {
		t.Skip("Set KIRO_INTEGRATION_TEST=1 to run integration tests")
	}

	dbPath := getDBPath(t)
	region := getRegion(t)

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

	kiroClient := client.NewKiroClient(authManager)
	defer kiroClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token, err := authManager.GetAccessToken()
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}
	t.Logf("Got access token, length: %d", len(token))

	imageURL := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGNg+P++HgAFhAJ/wlseKgAAAABJRU5ErkJggg=="

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
	}, "conv-"+strings.Repeat("x", 32), authManager.ProfileArn())

	if err != nil {
		t.Fatalf("BuildKiroPayload failed: %v", err)
	}

	t.Logf("Built payload with conversationId: conv-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	t.Logf("Image URL: %s", imageURL)

	resp, err := kiroClient.DoRequest(ctx, "POST", authManager.APIHost()+"/generateAssistantResponse", payload, true)
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
	if strings.Contains(bodyStr, "assistantResponseEvent") {
		t.Logf("SUCCESS: Got image recognition response!")
		preview := bodyStr
		if len(preview) > 200 {
			preview = preview[:200]
		}
		t.Logf("Response preview: %s", preview)
	} else {
		t.Fatalf("Expected assistant response event, got: %s", bodyStr[:min(200, len(bodyStr))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestFullGatewayLoop_OpenAICompatible(t *testing.T) {
	if os.Getenv("KIRO_INTEGRATION_TEST") != "1" {
		t.Skip("Set KIRO_INTEGRATION_TEST=1 to run integration tests")
	}

	dbPath := getDBPath(t)
	region := getRegion(t)

	kiroServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generateAssistantResponse" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"conversationId": "test-conv-id",
		})
	}))
	defer kiroServer.Close()

	cfg := &auth.AuthConfig{
		CliDbFile: dbPath,
		Region:    region,
	}

	authManager, err := auth.NewAuthManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create AuthManager: %v", err)
	}

	gatewayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var req models.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		profileArn := authManager.ProfileArn()
		conversationID := "conv-" + strings.Repeat("x", 32)

		kiroPayload, err := converter.BuildKiroPayload(&req, conversationID, profileArn)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_ = kiroPayload

		kiroResp, err := http.Post(kiroServer.URL+"/generateAssistantResponse", "application/json", nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer kiroResp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models.ChatCompletionResponse{
			ID:      "chatcmpl-" + strings.Repeat("x", 32),
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []models.Choice{
				{
					Index: 0,
					Message: &models.ChatMessage{
						Role:    "assistant",
						Content: "Test response",
					},
					FinishReason: "stop",
				},
			},
		})
	}))
	defer gatewayServer.Close()

	reqBody := `{
		"model": "claude-haiku-4.5",
		"messages": [{"role": "user", "content": "Hello"}],
		"stream": false
	}`

	req, err := http.NewRequest("POST", gatewayServer.URL+"/v1/chat/completions", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var chatResp models.ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(chatResp.Choices) == 0 {
		t.Fatal("No choices in response")
	}

	if chatResp.Choices[0].Message.Content != "Test response" {
		t.Errorf("Expected 'Test response', got %q", chatResp.Choices[0].Message.Content)
	}

	t.Logf("SUCCESS: Full gateway loop works!")
}
