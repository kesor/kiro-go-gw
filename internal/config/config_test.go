package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// Clear all env vars
	clearConfigEnv()

	cfg := Load()

	if cfg.ServerHost != "0.0.0.0" {
		t.Errorf("ServerHost: got %q, want %q", cfg.ServerHost, "0.0.0.0")
	}
	if cfg.ServerPort != 8000 {
		t.Errorf("ServerPort: got %d, want %d", cfg.ServerPort, 8000)
	}
	if cfg.ProxyAPIKey != "" {
		t.Errorf("ProxyAPIKey: got %q, want empty (required, no default)", cfg.ProxyAPIKey)
	}
	if cfg.Region != "us-east-1" {
		t.Errorf("Region: got %q, want %q", cfg.Region, "us-east-1")
	}
}

func TestLoadFromEnv(t *testing.T) {
	clearConfigEnv()

	os.Setenv("SERVER_HOST", "127.0.0.1")
	os.Setenv("SERVER_PORT", "9000")
	os.Setenv("PROXY_API_KEY", "test-key-123")
	os.Setenv("REFRESH_TOKEN", "test-token")
	os.Setenv("PROFILE_ARN", "arn:aws:test:profile")
	os.Setenv("KIRO_REGION", "eu-west-1")
	os.Setenv("KIRO_CLIENT_ID", "client-123")
	os.Setenv("KIRO_CLIENT_SECRET", "secret-456")
	defer clearConfigEnv()

	cfg := Load()

	if cfg.ServerHost != "127.0.0.1" {
		t.Errorf("ServerHost: got %q, want %q", cfg.ServerHost, "127.0.0.1")
	}
	if cfg.ServerPort != 9000 {
		t.Errorf("ServerPort: got %d, want %d", cfg.ServerPort, 9000)
	}
	if cfg.ProxyAPIKey != "test-key-123" {
		t.Errorf("ProxyAPIKey: got %q, want %q", cfg.ProxyAPIKey, "test-key-123")
	}
	if cfg.RefreshToken != "test-token" {
		t.Errorf("RefreshToken: got %q, want %q", cfg.RefreshToken, "test-token")
	}
	if cfg.ProfileArn != "arn:aws:test:profile" {
		t.Errorf("ProfileArn: got %q, want %q", cfg.ProfileArn, "arn:aws:test:profile")
	}
	if cfg.Region != "eu-west-1" {
		t.Errorf("Region: got %q, want %q", cfg.Region, "eu-west-1")
	}
	if cfg.ClientID != "client-123" {
		t.Errorf("ClientID: got %q, want %q", cfg.ClientID, "client-123")
	}
	if cfg.ClientSecret != "secret-456" {
		t.Errorf("ClientSecret: got %q, want %q", cfg.ClientSecret, "secret-456")
	}
}

func TestExpandPath(t *testing.T) {
	clearConfigEnv()

	// Test empty string
	os.Setenv("KIRO_CREDS_FILE", "")
	cfg := Load()
	if cfg.CredsFile != "" {
		t.Errorf("Empty path: got %q, want %q", cfg.CredsFile, "")
	}

	// Test absolute path
	os.Setenv("KIRO_CREDS_FILE", "/absolute/path/to/file.json")
	cfg = Load()
	if cfg.CredsFile != "/absolute/path/to/file.json" {
		t.Errorf("Absolute path: got %q, want %q", cfg.CredsFile, "/absolute/path/to/file.json")
	}

	// Test tilde expansion
	home, _ := os.UserHomeDir()
	os.Setenv("KIRO_CREDS_FILE", "~/relative/path.json")
	cfg = Load()
	expected := filepath.Join(home, "relative/path.json")
	if cfg.CredsFile != expected {
		t.Errorf("Tilde path: got %q, want %q", cfg.CredsFile, expected)
	}
}

func TestInvalidPort(t *testing.T) {
	clearConfigEnv()

	// Test non-numeric port
	os.Setenv("SERVER_PORT", "not-a-number")
	cfg := Load()
	if cfg.ServerPort != 8000 {
		t.Errorf("Non-numeric port: got %d, want %d (default)", cfg.ServerPort, 8000)
	}

	// Test empty port (should use default)
	os.Setenv("SERVER_PORT", "")
	cfg = Load()
	if cfg.ServerPort != 8000 {
		t.Errorf("Empty port: got %d, want %d", cfg.ServerPort, 8000)
	}
}

func TestCredsFilePath(t *testing.T) {
	clearConfigEnv()

	// Test that CredsFile gets path expansion
	os.Setenv("KIRO_CREDS_FILE", "~/creds.json")
	cfg := Load()
	if cfg.CredsFile == "" || cfg.CredsFile == "~/creds.json" {
		t.Errorf("CredsFile not expanded: got %q", cfg.CredsFile)
	}

	// Test CliDbFile gets path expansion
	os.Setenv("KIRO_CLI_DB_FILE", "~/Library/Application Support/kiro-cli/data.sqlite3")
	cfg = Load()
	if cfg.CliDbFile == "" || cfg.CliDbFile == "~/Library/Application Support/kiro-cli/data.sqlite3" {
		t.Errorf("CliDbFile not expanded: got %q", cfg.CliDbFile)
	}
}

func clearConfigEnv() {
	os.Unsetenv("SERVER_HOST")
	os.Unsetenv("SERVER_PORT")
	os.Unsetenv("PROXY_API_KEY")
	os.Unsetenv("REFRESH_TOKEN")
	os.Unsetenv("PROFILE_ARN")
	os.Unsetenv("KIRO_REGION")
	os.Unsetenv("KIRO_CREDS_FILE")
	os.Unsetenv("KIRO_CLI_DB_FILE")
	os.Unsetenv("KIRO_CLIENT_ID")
	os.Unsetenv("KIRO_CLIENT_SECRET")
}
