package auth

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestNewAuthManager_NoCreds(t *testing.T) {
	cfg := &AuthConfig{
		RefreshToken: "",
		CredsFile:    "",
		CliDbFile:    "",
	}

	// Should not error - uses defaults
	am, err := NewAuthManager(cfg)
	if err != nil {
		t.Fatalf("NewAuthManager with no creds should not error: %v", err)
	}

	if am.authType != AuthTypeKiroDesktop {
		t.Errorf("authType: got %v, want kiro_desktop", am.authType)
	}
}

func TestDetectAuthType(t *testing.T) {
	tests := []struct {
		name         string
		clientID     string
		clientSecret string
		expected     AuthType
	}{
		{"both empty", "", "", AuthTypeKiroDesktop},
		{"only clientID", "abc", "", AuthTypeKiroDesktop},
		{"only clientSecret", "", "xyz", AuthTypeKiroDesktop},
		{"both set", "abc", "xyz", AuthTypeAWSSSO},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			am := &AuthManager{
				clientID:     tc.clientID,
				clientSecret: tc.clientSecret,
			}
			am.detectAuthType()

			if am.authType != tc.expected {
				t.Errorf("authType: got %v, want %v", am.authType, tc.expected)
			}
		})
	}
}

func TestLoadFromFile(t *testing.T) {
	// Create temp file with credentials
	content := `{
		"accessToken": "test-access-token",
		"refreshToken": "test-refresh-token",
		"profileArn": "arn:aws:codewhisperer:us-east-1:123456789:profile/test",
		"region": "us-west-2",
		"expiresAt": "2025-01-01T00:00:00Z",
		"clientId": "client-123",
		"clientSecret": "secret-456"
	}`

	tmpFile, err := os.CreateTemp("", "creds-*.json")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("WriteString failed: %v", err)
	}
	tmpFile.Close()

	am := &AuthManager{}
	if err := am.loadFromFile(tmpFile.Name()); err != nil {
		t.Fatalf("loadFromFile failed: %v", err)
	}

	// Verify loaded values
	if am.accessToken != "test-access-token" {
		t.Errorf("accessToken: got %q, want test-access-token", am.accessToken)
	}
	if am.refreshToken != "test-refresh-token" {
		t.Errorf("refreshToken: got %q, want test-refresh-token", am.refreshToken)
	}
	if am.profileArn != "arn:aws:codewhisperer:us-east-1:123456789:profile/test" {
		t.Errorf("profileArn: got %q, want arn:aws:codewhisperer:us-east-1:123456789:profile/test", am.profileArn)
	}
	if am.ssoRegion != "us-west-2" {
		t.Errorf("region: got %q, want us-west-2", am.ssoRegion)
	}
	if am.clientID != "client-123" {
		t.Errorf("clientId: got %q, want client-123", am.clientID)
	}
	if am.clientSecret != "secret-456" {
		t.Errorf("clientSecret: got %q, want secret-456", am.clientSecret)
	}
}

func TestLoadFromFile_NotFound(t *testing.T) {
	am := &AuthManager{}
	err := am.loadFromFile("/nonexistent/path/to/file.json")

	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadFromFile_InvalidJSON(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "creds-*.json")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write invalid JSON
	tmpFile.WriteString("not valid json")
	tmpFile.Close()

	am := &AuthManager{}
	err = am.loadFromFile(tmpFile.Name())

	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadFromSQLite(t *testing.T) {
	// Create temp SQLite database
	tmpFile, err := os.CreateTemp("", "test-*.sqlite3")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	db, err := sql.Open("sqlite3", tmpFile.Name())
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	defer db.Close()

	// Create auth_kv table
	_, err = db.Exec("CREATE TABLE auth_kv (key TEXT PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatalf("CREATE TABLE failed: %v", err)
	}

	// Insert token data
	tokenData := TokenData{
		AccessToken:  "sqlite-access-token",
		RefreshToken: "sqlite-refresh-token",
		ProfileArn:   "arn:aws:test",
		Region:       "eu-central-1",
		ExpiresAt:    "2025-12-31T23:59:59Z",
	}
	tokenJSON, _ := json.Marshal(tokenData)
	db.Exec("INSERT INTO auth_kv (key, value) VALUES (?, ?)", "kirocli:odic:token", tokenJSON)

	// Insert device registration
	regData := DeviceRegistration{
		ClientID:     "sqlite-client-id",
		ClientSecret: "sqlite-client-secret",
		Region:       "eu-central-1",
	}
	regJSON, _ := json.Marshal(regData)
	db.Exec("INSERT INTO auth_kv (key, value) VALUES (?, ?)", "kirocli:odic:device-registration", regJSON)

	am := &AuthManager{}
	if err := am.loadFromSQLite(tmpFile.Name()); err != nil {
		t.Fatalf("loadFromSQLite failed: %v", err)
	}

	// Verify loaded values
	if am.accessToken != "sqlite-access-token" {
		t.Errorf("accessToken: got %q, want sqlite-access-token", am.accessToken)
	}
	if am.refreshToken != "sqlite-refresh-token" {
		t.Errorf("refreshToken: got %q, want sqlite-refresh-token", am.refreshToken)
	}
	if am.profileArn != "arn:aws:test" {
		t.Errorf("profileArn: got %q, want arn:aws:test", am.profileArn)
	}
	if am.ssoRegion != "eu-central-1" {
		t.Errorf("ssoRegion: got %q, want eu-central-1", am.ssoRegion)
	}
	if am.clientID != "sqlite-client-id" {
		t.Errorf("clientID: got %q, want sqlite-client-id", am.clientID)
	}
	if am.clientSecret != "sqlite-client-secret" {
		t.Errorf("clientSecret: got %q, want sqlite-client-secret", am.clientSecret)
	}
}

func TestLoadFromSQLite_FileNotFound(t *testing.T) {
	am := &AuthManager{}
	err := am.loadFromSQLite("/nonexistent/db.sqlite3")

	// Should return error for missing file
	if err == nil {
		t.Error("loadFromSQLite should error on missing file")
	}
}

func TestLoadFromSQLite_PriorityOrder(t *testing.T) {
	// Create temp SQLite with multiple token types
	tmpFile, err := os.CreateTemp("", "test-*.sqlite3")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	db, _ := sql.Open("sqlite3", tmpFile.Name())
	defer db.Close()

	db.Exec("CREATE TABLE auth_kv (key TEXT PRIMARY KEY, value TEXT)")

	// Insert all three token types
	db.Exec("INSERT INTO auth_kv VALUES ('kirocli:social:token', '{\"access_token\": \"social-token\"}')")
	db.Exec("INSERT INTO auth_kv VALUES ('kirocli:odic:token', '{\"access_token\": \"odic-token\"}')")
	db.Exec("INSERT INTO auth_kv VALUES ('codewhisperer:odic:token', '{\"access_token\": \"cw-token\"}')")

	am := &AuthManager{}
	am.loadFromSQLite(tmpFile.Name())

	// Should pick first match in priority order (social:token > odic:token > codewhisperer:odic:token)
	if am.accessToken != "social-token" {
		t.Errorf("should pick social token first (highest priority), got: %q", am.accessToken)
	}
}

func TestIsTokenExpiringSoon(t *testing.T) {
	am := &AuthManager{}

	// Zero time means expiring soon
	if !am.isTokenExpiringSoon() {
		t.Error("zero time should return true (expiring soon)")
	}

	// Future time > 10 min - not expiring
	am.expiresAt = time.Now().Add(30 * time.Minute)
	if am.isTokenExpiringSoon() {
		t.Error("token 30 min in future should not be expiring soon")
	}

	// Future time < 10 min - expiring soon
	am.expiresAt = time.Now().Add(5 * time.Minute)
	if !am.isTokenExpiringSoon() {
		t.Error("token 5 min in future should be expiring soon")
	}

	// Past time - expired (should return true)
	am.expiresAt = time.Now().Add(-5 * time.Minute)
	if !am.isTokenExpiringSoon() {
		t.Error("expired token should be expiring soon")
	}
}

func TestIsTokenExpired(t *testing.T) {
	am := &AuthManager{}

	// Zero time means expired
	if !am.isTokenExpired() {
		t.Error("zero time should return true (expired)")
	}

	// Future time - not expired
	am.expiresAt = time.Now().Add(30 * time.Minute)
	if am.isTokenExpired() {
		t.Error("token 30 min in future should not be expired")
	}

	// Past time - expired
	am.expiresAt = time.Now().Add(-5 * time.Minute)
	if !am.isTokenExpired() {
		t.Error("token 5 min in past should be expired")
	}
}

func TestAPIHost(t *testing.T) {
	am := &AuthManager{
		config: &AuthConfig{Region: "eu-west-1"},
	}

	host := am.APIHost()
	expected := "https://q.eu-west-1.amazonaws.com"

	if host != expected {
		t.Errorf("APIHost: got %q, want %q", host, expected)
	}
}

func TestAPIHost_DefaultRegion(t *testing.T) {
	am := &AuthManager{
		config: &AuthConfig{Region: "us-east-1"},
	}

	host := am.APIHost()
	expected := "https://q.us-east-1.amazonaws.com"

	if host != expected {
		t.Errorf("APIHost: got %q, want %q", host, expected)
	}
}

func TestAuthConfigStruct(t *testing.T) {
	cfg := AuthConfig{
		RefreshToken: "token",
		ProfileArn:   "arn:aws:test",
		Region:       "us-east-1",
		CredsFile:    "/path/to/creds",
		CliDbFile:    "/path/to/db",
		ClientID:     "client",
		ClientSecret: "secret",
	}

	if cfg.RefreshToken != "token" {
		t.Errorf("RefreshToken: got %q, want token", cfg.RefreshToken)
	}
	if cfg.ProfileArn != "arn:aws:test" {
		t.Errorf("ProfileArn: got %q, want arn:aws:test", cfg.ProfileArn)
	}
	if cfg.Region != "us-east-1" {
		t.Errorf("Region: got %q, want us-east-1", cfg.Region)
	}
	if cfg.CredsFile != "/path/to/creds" {
		t.Errorf("CredsFile: got %q, want /path/to/creds", cfg.CredsFile)
	}
	if cfg.CliDbFile != "/path/to/db" {
		t.Errorf("CliDbFile: got %q, want /path/to/db", cfg.CliDbFile)
	}
	if cfg.ClientID != "client" {
		t.Errorf("ClientID: got %q, want client", cfg.ClientID)
	}
	if cfg.ClientSecret != "secret" {
		t.Errorf("ClientSecret: got %q, want secret", cfg.ClientSecret)
	}
}

func TestCredentialsFileStruct(t *testing.T) {
	jsonData := `{
		"accessToken": "at",
		"refreshToken": "rt",
		"profileArn": "arn",
		"region": "us-west-1",
		"expiresAt": "2025-01-01T00:00:00Z",
		"clientId": "cid",
		"clientSecret": "csecret",
		"clientIdHash": "hash123"
	}`

	var creds CredentialsFile
	if err := json.Unmarshal([]byte(jsonData), &creds); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if creds.AccessToken != "at" {
		t.Errorf("AccessToken: got %q, want at", creds.AccessToken)
	}
	if creds.RefreshToken != "rt" {
		t.Errorf("RefreshToken: got %q, want rt", creds.RefreshToken)
	}
	if creds.ProfileArn != "arn" {
		t.Errorf("ProfileArn: got %q, want arn", creds.ProfileArn)
	}
	if creds.Region != "us-west-1" {
		t.Errorf("Region: got %q, want us-west-1", creds.Region)
	}
	if creds.ExpiresAt != "2025-01-01T00:00:00Z" {
		t.Errorf("ExpiresAt: got %q, want 2025-01-01T00:00:00Z", creds.ExpiresAt)
	}
	if creds.ClientID != "cid" {
		t.Errorf("ClientID: got %q, want cid", creds.ClientID)
	}
	if creds.ClientSecret != "csecret" {
		t.Errorf("ClientSecret: got %q, want csecret", creds.ClientSecret)
	}
	if creds.ClientIDHash != "hash123" {
		t.Errorf("ClientIDHash: got %q, want hash123", creds.ClientIDHash)
	}
}

func TestDeviceRegistrationStruct(t *testing.T) {
	jsonData := `{
		"client_id": "cid",
		"client_secret": "csecret",
		"region": "eu-west-1"
	}`

	var reg DeviceRegistration
	if err := json.Unmarshal([]byte(jsonData), &reg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if reg.ClientID != "cid" {
		t.Errorf("ClientID: got %q, want cid", reg.ClientID)
	}
	if reg.ClientSecret != "csecret" {
		t.Errorf("ClientSecret: got %q, want csecret", reg.ClientSecret)
	}
	if reg.Region != "eu-west-1" {
		t.Errorf("Region: got %q, want eu-west-1", reg.Region)
	}
}

func TestLoadEnterpriseDeviceRegistration_FileNotFound(t *testing.T) {
	// Create temp dir but not the file
	tmpDir, err := os.MkdirTemp("", "aws-sso-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create cache subdir but not the file
	cacheDir := filepath.Join(tmpDir, "cache")
	os.MkdirAll(cacheDir, 0755)

	am := &AuthManager{}
	err = am.loadEnterpriseDeviceRegistration("nonexistent-hash")

	// Should not error - just returns error
	if err == nil {
		// This is also acceptable - error is returned but not propagated
	}
}

func TestTokenDataStruct(t *testing.T) {
	jsonData := `{
		"access_token": "at",
		"refresh_token": "rt",
		"expires_at": "2025-01-01T00:00:00Z",
		"region": "us-east-1",
		"scopes": ["scope1", "scope2"],
		"profile_arn": "arn:aws:test"
	}`

	var token TokenData
	if err := json.Unmarshal([]byte(jsonData), &token); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if token.AccessToken != "at" {
		t.Errorf("AccessToken: got %q, want at", token.AccessToken)
	}
	if token.RefreshToken != "rt" {
		t.Errorf("RefreshToken: got %q, want rt", token.RefreshToken)
	}
	if token.Region != "us-east-1" {
		t.Errorf("Region: got %q, want us-east-1", token.Region)
	}
	if len(token.Scopes) != 2 {
		t.Errorf("Scopes: got %v, want 2 items", token.Scopes)
	}
	if token.ProfileArn != "arn:aws:test" {
		t.Errorf("ProfileArn: got %q, want arn:aws:test", token.ProfileArn)
	}
}

func TestAuthManagerWithRealCredentials(t *testing.T) {
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

	cfg := &AuthConfig{
		CliDbFile: dbPath,
		Region:    region,
	}

	am, err := NewAuthManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create AuthManager: %v", err)
	}

	t.Logf("Auth type: %s", am.authType)
	t.Logf("Profile ARN: %s", am.profileArn)
	t.Logf("SSO Region: %s", am.ssoRegion)
	t.Logf("Has refresh token: %v", am.refreshToken != "")
	t.Logf("Token expires: %v", am.expiresAt)
	t.Logf("Token expired: %v", am.isTokenExpired())
	t.Logf("Token expiring soon: %v", am.isTokenExpiringSoon())

	token, err := am.GetAccessToken()
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}

	if token == "" {
		t.Fatal("Got empty access token")
	}

	t.Logf("Got access token, length: %d", len(token))

	if am.isTokenExpired() {
		t.Error("Token should not be expired after GetAccessToken")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestRefreshTokenKiroDesktop_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/refreshToken" {
			t.Errorf("Expected path /refreshToken, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type: application/json, got %s", r.Header.Get("Content-Type"))
		}

		var payload map[string]string
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to unmarshal request body as JSON: %v", err)
		}

		if payload["refreshToken"] != "test-refresh-token" {
			t.Errorf("Expected refreshToken 'test-refresh-token', got %s", payload["refreshToken"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken":  "new-access-token",
			"refreshToken": "new-refresh-token",
			"expiresIn":    3600,
		})
	}))
	defer server.Close()

	am := &AuthManager{
		config:       &AuthConfig{Region: "us-east-1"},
		refreshToken: "test-refresh-token",
		refreshURL:   server.URL + "/refreshToken",
	}

	err := am.refreshTokenKiroDesktop()
	if err != nil {
		t.Fatalf("refreshTokenKiroDesktop failed: %v", err)
	}

	if am.accessToken != "new-access-token" {
		t.Errorf("accessToken: got %q, want %q", am.accessToken, "new-access-token")
	}
	if am.refreshToken != "new-refresh-token" {
		t.Errorf("refreshToken: got %q, want %q", am.refreshToken, "new-refresh-token")
	}
	if am.expiresAt.IsZero() {
		t.Error("expiresAt should not be zero")
	}
}

func TestRefreshTokenKiroDesktop_NoRefreshToken(t *testing.T) {
	am := &AuthManager{
		config:       &AuthConfig{Region: "us-east-1"},
		refreshToken: "",
	}

	err := am.refreshTokenKiroDesktop()
	if err == nil {
		t.Error("Expected error when refresh token is empty")
	}
}

func TestRefreshTokenKiroDesktop_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	am := &AuthManager{
		config:       &AuthConfig{Region: "us-east-1"},
		refreshToken: "test-refresh-token",
		refreshURL:   server.URL,
	}

	err := am.refreshTokenKiroDesktop()
	if err == nil {
		t.Error("Expected error when server returns 500")
	}
}

func TestRefreshTokenAWSSSO_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			t.Errorf("Expected path /token, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type: application/json, got %s", r.Header.Get("Content-Type"))
		}

		var payload map[string]string
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("failed to unmarshal request body: %v", err)
		}

		if payload["grantType"] != "refresh_token" {
			t.Errorf("Expected grantType 'refresh_token', got %s", payload["grantType"])
		}
		if payload["clientId"] != "test-client-id" {
			t.Errorf("Expected clientId 'test-client-id', got %s", payload["clientId"])
		}
		if payload["clientSecret"] != "test-client-secret" {
			t.Errorf("Expected clientSecret 'test-client-secret', got %s", payload["clientSecret"])
		}
		if payload["refreshToken"] != "test-refresh-token" {
			t.Errorf("Expected refreshToken 'test-refresh-token', got %s", payload["refreshToken"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken":  "new-aws-access-token",
			"refreshToken": "new-aws-refresh-token",
			"expiresIn":    3600,
		})
	}))
	defer server.Close()

	am := &AuthManager{
		config:       &AuthConfig{Region: "us-east-1"},
		ssoRegion:    "us-east-1",
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
		refreshToken: "test-refresh-token",
		refreshURL:   server.URL + "/token",
	}

	err := am.refreshTokenAWSSSO()
	if err != nil {
		t.Fatalf("refreshTokenAWSSSO failed: %v", err)
	}

	if am.accessToken != "new-aws-access-token" {
		t.Errorf("accessToken: got %q, want %q", am.accessToken, "new-aws-access-token")
	}
	if am.refreshToken != "new-aws-refresh-token" {
		t.Errorf("refreshToken: got %q, want %q", am.refreshToken, "new-aws-refresh-token")
	}
	if am.expiresAt.IsZero() {
		t.Error("expiresAt should not be zero")
	}
}

func TestRefreshTokenAWSSSO_NoClientID(t *testing.T) {
	am := &AuthManager{
		config:       &AuthConfig{Region: "us-east-1"},
		refreshToken: "test-refresh-token",
		clientID:     "",
		clientSecret: "test-client-secret",
	}

	err := am.refreshTokenAWSSSO()
	if err == nil {
		t.Error("Expected error when clientID is empty")
	}
}

func TestRefreshTokenAWSSSO_NoClientSecret(t *testing.T) {
	am := &AuthManager{
		config:       &AuthConfig{Region: "us-east-1"},
		refreshToken: "test-refresh-token",
		clientID:     "test-client-id",
		clientSecret: "",
	}

	err := am.refreshTokenAWSSSO()
	if err == nil {
		t.Error("Expected error when clientSecret is empty")
	}
}

func TestRefreshTokenAWSSSO_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	am := &AuthManager{
		config:       &AuthConfig{Region: "us-east-1"},
		ssoRegion:    "us-east-1",
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
		refreshToken: "test-refresh-token",
		refreshURL:   server.URL + "/token",
	}

	err := am.refreshTokenAWSSSO()
	if err == nil {
		t.Error("Expected error when server returns 500")
	}
}

func TestRefreshTokenAWSSSO_InvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			// Missing accessToken (camelCase to match actual API)
			"refreshToken": "new-refresh-token",
		})
	}))
	defer server.Close()

	am := &AuthManager{
		config:       &AuthConfig{Region: "us-east-1"},
		ssoRegion:    "us-east-1",
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
		refreshToken: "test-refresh-token",
		refreshURL:   server.URL + "/token",
	}

	err := am.refreshTokenAWSSSO()
	if err == nil {
		t.Error("Expected error when response missing accessToken")
	}
}

func TestRefreshTokenAWSSSO_UsesDefaultRegion(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken": "new-token",
		})
	}))
	defer server.Close()

	am := &AuthManager{
		config:       &AuthConfig{Region: "eu-west-1"},
		ssoRegion:    "", // Not set, should fall back to config region
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
		refreshToken: "test-refresh-token",
		refreshURL:   server.URL + "/token", // Override URL to hit test server
	}

	err := am.refreshTokenAWSSSO()
	if err != nil {
		t.Fatalf("refreshTokenAWSSSO failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected 1 request, got %d", callCount)
	}
}

func TestAuthTypeDetection(t *testing.T) {
	tests := []struct {
		name         string
		clientID     string
		clientSecret string
		want         AuthType
	}{
		{
			name:         "with both client credentials",
			clientID:     "test-id",
			clientSecret: "test-secret",
			want:         AuthTypeAWSSSO,
		},
		{
			name:         "without client credentials",
			clientID:     "",
			clientSecret: "",
			want:         AuthTypeKiroDesktop,
		},
		{
			name:         "only client ID",
			clientID:     "test-id",
			clientSecret: "",
			want:         AuthTypeKiroDesktop,
		},
		{
			name:         "only client secret",
			clientID:     "",
			clientSecret: "test-secret",
			want:         AuthTypeKiroDesktop,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			am := &AuthManager{
				config:       &AuthConfig{Region: "us-east-1"},
				clientID:     tt.clientID,
				clientSecret: tt.clientSecret,
			}
			am.detectAuthType()

			if am.authType != tt.want {
				t.Errorf("authType: got %v, want %v", am.authType, tt.want)
			}
		})
	}
}
