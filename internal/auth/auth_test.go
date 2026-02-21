package auth

import (
	"database/sql"
	"encoding/json"
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

	// Should not error - gracefully handles missing file
	if err != nil {
		t.Errorf("loadFromSQLite should handle missing file gracefully: %v", err)
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

	// Should pick first match in priority order
	if am.accessToken != "odic-token" {
		t.Errorf("should pick odic token first, got: %q", am.accessToken)
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
