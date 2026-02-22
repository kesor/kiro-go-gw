package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	Debug        bool
	DebugLogFile string
}

func Load() *Config {
	expandPath := func(p string) string {
		if p == "" {
			return ""
		}
		if p[0] == '~' {
			home, err := os.UserHomeDir()
			if err != nil {
				return p // Return unchanged if home unavailable
			}
			cleanPath := filepath.Clean(filepath.Join(home, p[1:]))
			// Verify the path is still within home directory
			if !strings.HasPrefix(cleanPath, home+string(filepath.Separator)) && cleanPath != home {
				return p // Path traversal attempt, return unchanged
			}
			return cleanPath
		}
		// Also clean and validate absolute paths
		return filepath.Clean(p)
	}

	return &Config{
		ServerHost:   getEnv("SERVER_HOST", "0.0.0.0"),
		ServerPort:   getEnvInt("SERVER_PORT", 8000),
		ProxyAPIKey:  getEnv("PROXY_API_KEY", ""),
		RefreshToken: getEnv("REFRESH_TOKEN", ""),
		ProfileArn:   getEnv("PROFILE_ARN", ""),
		Region:       getEnv("KIRO_REGION", "us-east-1"),
		CredsFile:    expandPath(getEnv("KIRO_CREDS_FILE", "")),
		CliDbFile:    expandPath(getEnv("KIRO_CLI_DB_FILE", "")),
		ClientID:     getEnv("KIRO_CLIENT_ID", ""),
		ClientSecret: getEnv("KIRO_CLIENT_SECRET", ""),
		Debug:        getEnv("DEBUG", "false") == "true",
		DebugLogFile: getEnv("DEBUG_LOG_FILE", ""),
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
