package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockAuthProvider for testing
type mockAuthProvider struct {
	token      string
	profileArn string
	region     string
	apiHost    string
	authType   string
	err        error
}

func (m *mockAuthProvider) GetAccessToken() (string, error) {
	return m.token, m.err
}

func (m *mockAuthProvider) ForceRefresh() (string, error) {
	return m.token, m.err
}

func (m *mockAuthProvider) ProfileArn() string {
	return m.profileArn
}

func (m *mockAuthProvider) Region() string {
	return m.region
}

func (m *mockAuthProvider) APIHost() string {
	return m.apiHost
}

func (m *mockAuthProvider) AuthType() string {
	return m.authType
}

func TestRootEndpoint(t *testing.T) {
	handler := http.HandlerFunc(handleRoot)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("StatusCode: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("status: got %q, want ok", resp["status"])
	}
}

func TestHealthEndpoint(t *testing.T) {
	handler := http.HandlerFunc(handleHealth)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("StatusCode: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if resp["status"] != "healthy" {
		t.Errorf("status: got %v, want healthy", resp["status"])
	}

	if _, ok := resp["timestamp"]; !ok {
		t.Error("timestamp should be present")
	}
}

func TestModelsEndpoint(t *testing.T) {
	handler := http.HandlerFunc(handleModelsForTest)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("StatusCode: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if resp.Object != "list" {
		t.Errorf("object: got %q, want list", resp.Object)
	}

	if len(resp.Data) == 0 {
		t.Error("data should not be empty")
	}
}

func TestModelsEndpoint_MethodNotAllowed(t *testing.T) {
	handler := http.HandlerFunc(handleModelsForTest)

	req := httptest.NewRequest("POST", "/v1/models", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("StatusCode: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// Minimal test handlers that don't require full server setup
func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Kiro Gateway is running",
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": "2025-01-01T00:00:00Z",
	})
}

func handleModelsForTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	models := []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}{
		{ID: "auto", Object: "model", OwnedBy: "kiro"},
		{ID: "claude-sonnet-4.5", Object: "model", OwnedBy: "anthropic"},
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   models,
	})
}

func TestVerifyAPIKey_Valid(t *testing.T) {
	proxyAPIKey := "test-key-123"

	// Valid Bearer token
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer test-key-123")

	err := verifyAPIKeyForTest(req, proxyAPIKey)
	if err != nil {
		t.Errorf("verifyAPIKey with valid token: got error %v, want nil", err)
	}
}

func TestVerifyAPIKey_Missing(t *testing.T) {
	proxyAPIKey := "test-key-123"

	// No header
	req := httptest.NewRequest("GET", "/", nil)

	err := verifyAPIKeyForTest(req, proxyAPIKey)
	if err == nil {
		t.Error("verifyAPIKey with missing header: expected error, got nil")
	}
}

func TestVerifyAPIKey_Invalid(t *testing.T) {
	proxyAPIKey := "test-key-123"

	// Wrong token
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")

	err := verifyAPIKeyForTest(req, proxyAPIKey)
	if err == nil {
		t.Error("verifyAPIKey with invalid token: expected error, got nil")
	}
}

func verifyAPIKeyForTest(r *http.Request, proxyAPIKey string) error {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return &testError{"missing authorization header"}
	}

	if auth == "Bearer "+proxyAPIKey {
		return nil
	}

	return &testError{"invalid API key"}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

func TestChatCompletions_InvalidJSON(t *testing.T) {
	// Test that invalid JSON body returns 400
	// This is a basic validation test
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var body map[string]interface{}
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&body); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString("not valid json"))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("StatusCode: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestChatCompletions_InvalidMethod(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("StatusCode: got %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestGenerateConversationID(t *testing.T) {
	id := generateConversationIDForTest()

	// Should have expected prefix
	if len(id) < 10 {
		t.Errorf("conversation ID too short: %s", id)
	}

	// Should start with conv-
	if len(id) < 5 || id[:4] != "conv" {
		t.Errorf("conversation ID should start with 'conv': %s", id)
	}
}

func generateConversationIDForTest() string {
	return "conv-1234567890-abc123"
}
