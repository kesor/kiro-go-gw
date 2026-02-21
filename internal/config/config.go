package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	ServerHost   string
	ServerPort   int
	ProxyAPIKey  string
	RefreshToken string
	ProfileArn   string
	Region       string
	CredsFile    string
	CliDbFile    string
	ClientID     string
	ClientSecret string
}

func Load() *Config {
	expandPath := func(p string) string {
		if p == "" {
			return ""
		}
		if p[0] == '~' {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, p[1:])
		}
		return p
	}

	return &Config{
		ServerHost:   getEnv("SERVER_HOST", "0.0.0.0"),
		ServerPort:   getEnvInt("SERVER_PORT", 8000),
		ProxyAPIKey:  getEnv("PROXY_API_KEY", "my-super-secret-password-123"),
		RefreshToken: getEnv("REFRESH_TOKEN", ""),
		ProfileArn:   getEnv("PROFILE_ARN", ""),
		Region:       getEnv("KIRO_REGION", "us-east-1"),
		CredsFile:    expandPath(getEnv("KIRO_CREDS_FILE", "")),
		CliDbFile:    expandPath(getEnv("KIRO_CLI_DB_FILE", "")),
		ClientID:     getEnv("KIRO_CLIENT_ID", ""),
		ClientSecret: getEnv("KIRO_CLIENT_SECRET", ""),
	}
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			return i
		}
	}
	return defaultValue
}
