package auth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// AuthProvider defines the interface for authentication.
// This allows for mocking in tests.
type AuthProvider interface {
	GetAccessToken() (string, error)
	ForceRefresh() (string, error)
	ProfileArn() string
	Region() string
	APIHost() string
}

type AuthType string

const (
	AuthTypeKiroDesktop AuthType = "kiro_desktop"
	AuthTypeAWSSSO      AuthType = "aws_sso"
)

var (
	tokenKeys = []string{
		"kirocli:social:token",
		"kirocli:odic:token",
		"codewhisperer:odic:token",
	}
	registrationKeys = []string{
		"kirocli:odic:device-registration",
		"codewhisperer:odic:device-registration",
	}
)

type TokenData struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	ExpiresAt    string   `json:"expires_at"`
	Region       string   `json:"region"`
	Scopes       []string `json:"scopes"`
	ProfileArn   string   `json:"profile_arn"`
}

type DeviceRegistration struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Region       string `json:"region"`
}

type CredentialsFile struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ProfileArn   string `json:"profileArn"`
	Region       string `json:"region"`
	ExpiresAt    string `json:"expiresAt"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	ClientIDHash string `json:"clientIdHash"`
}

type AuthManager struct {
	mu sync.RWMutex

	config       *AuthConfig
	authType     AuthType
	accessToken  string
	refreshToken string
	expiresAt    time.Time
	clientID     string
	clientSecret string
	ssoRegion    string
	profileArn   string

	refreshURL string
}

type AuthConfig struct {
	RefreshToken string
	ProfileArn   string
	Region       string
	CredsFile    string
	CliDbFile    string
	ClientID     string
	ClientSecret string
	RefreshURL   string
}

func NewAuthManager(cfg *AuthConfig) (*AuthManager, error) {
	am := &AuthManager{
		config:     cfg,
		authType:   AuthTypeKiroDesktop,
		profileArn: cfg.ProfileArn,
		refreshURL: cfg.RefreshURL,
	}

	// Load credentials based on priority
	if cfg.CredsFile != "" {
		if err := am.loadFromFile(cfg.CredsFile); err != nil {
			return nil, fmt.Errorf("failed to load credentials from file: %w", err)
		}
	} else if cfg.CliDbFile != "" {
		if err := am.loadFromSQLite(cfg.CliDbFile); err != nil {
			return nil, fmt.Errorf("failed to load credentials from SQLite: %w", err)
		}
	} else if cfg.RefreshToken != "" {
		am.refreshToken = cfg.RefreshToken
	}

	// Override with explicit client credentials if provided
	if cfg.ClientID != "" {
		am.clientID = cfg.ClientID
	}
	if cfg.ClientSecret != "" {
		am.clientSecret = cfg.ClientSecret
	}

	// Detect auth type
	am.detectAuthType()

	return am, nil
}

func (am *AuthManager) detectAuthType() {
	if am.clientID != "" && am.clientSecret != "" {
		am.authType = AuthTypeAWSSSO
		return
	}
	am.authType = AuthTypeKiroDesktop
}

func (am *AuthManager) loadFromFile(filePath string) error {
	data, err := os.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return err
	}

	var creds CredentialsFile
	if err := json.Unmarshal(data, &creds); err != nil {
		return err
	}

	if creds.RefreshToken != "" {
		am.refreshToken = creds.RefreshToken
	}
	if creds.AccessToken != "" {
		am.accessToken = creds.AccessToken
	}
	if creds.ProfileArn != "" {
		am.profileArn = creds.ProfileArn
	}
	if creds.Region != "" {
		am.ssoRegion = creds.Region
	}
	if creds.ClientID != "" {
		am.clientID = creds.ClientID
	}
	if creds.ClientSecret != "" {
		am.clientSecret = creds.ClientSecret
	}
	if creds.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, creds.ExpiresAt); err == nil {
			am.expiresAt = t
		}
	}

	// Handle enterprise device registration
	if creds.ClientIDHash != "" {
		if err := am.loadEnterpriseDeviceRegistration(creds.ClientIDHash); err != nil {
			log.Printf("Warning: failed to load enterprise device registration: %v", err)
		}
	}

	return nil
}

func (am *AuthManager) loadEnterpriseDeviceRegistration(clientIDHash string) error {
	home, _ := os.UserHomeDir()
	deviceRegPath := filepath.Join(home, ".aws", "sso", "cache", clientIDHash+".json")

	data, err := os.ReadFile(deviceRegPath)
	if err != nil {
		return err
	}

	var reg DeviceRegistration
	if err := json.Unmarshal(data, &reg); err != nil {
		return err
	}

	if reg.ClientID != "" {
		am.clientID = reg.ClientID
	}
	if reg.ClientSecret != "" {
		am.clientSecret = reg.ClientSecret
	}
	if reg.Region != "" && am.ssoRegion == "" {
		am.ssoRegion = reg.Region
	}

	return nil
}

func (am *AuthManager) loadFromSQLite(dbPath string) error {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open SQLite database: %w", err)
	}
	defer db.Close()

	// Verify connection works
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping SQLite database: %w", err)
	}

	// Try to load token
	for _, key := range tokenKeys {
		var tokenJSON string
		err := db.QueryRow("SELECT value FROM auth_kv WHERE key = ?", key).Scan(&tokenJSON)
		if err == nil {
			var tokenData TokenData
			if err := json.Unmarshal([]byte(tokenJSON), &tokenData); err == nil {
				am.accessToken = tokenData.AccessToken
				am.refreshToken = tokenData.RefreshToken
				am.profileArn = tokenData.ProfileArn
				if tokenData.Region != "" {
					am.ssoRegion = tokenData.Region
				}
				if tokenData.ExpiresAt != "" {
					if t, err := time.Parse(time.RFC3339, tokenData.ExpiresAt); err == nil {
						am.expiresAt = t
					}
				}
				break
			}
		}
	}

	// Try to load device registration
	for _, key := range registrationKeys {
		var regJSON string
		err := db.QueryRow("SELECT value FROM auth_kv WHERE key = ?", key).Scan(&regJSON)
		if err == nil {
			var reg DeviceRegistration
			if err := json.Unmarshal([]byte(regJSON), &reg); err == nil {
				if reg.ClientID != "" {
					am.clientID = reg.ClientID
				}
				if reg.ClientSecret != "" {
					am.clientSecret = reg.ClientSecret
				}
				if reg.Region != "" && am.ssoRegion == "" {
					am.ssoRegion = reg.Region
				}
				break
			}
		}
	}

	return nil
}

func (am *AuthManager) isTokenExpiringSoon() bool {
	if am.expiresAt.IsZero() {
		return true
	}
	return time.Until(am.expiresAt) < 10*time.Minute
}

func (am *AuthManager) isTokenExpired() bool {
	if am.expiresAt.IsZero() {
		return true
	}
	return time.Now().After(am.expiresAt)
}

func (am *AuthManager) GetAccessToken() (string, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.accessToken != "" && !am.isTokenExpiringSoon() {
		return am.accessToken, nil
	}

	if err := am.refreshTokenLocked(); err != nil {
		return "", err
	}

	return am.accessToken, nil
}

func (am *AuthManager) refreshTokenLocked() error {
	switch am.authType {
	case AuthTypeAWSSSO:
		return am.refreshTokenAWSSSO()
	default:
		return am.refreshTokenKiroDesktop()
	}
}

func (am *AuthManager) refreshTokenKiroDesktop() error {
	if am.refreshToken == "" {
		return fmt.Errorf("refresh token is not set")
	}

	refreshURL := fmt.Sprintf("https://prod.%s.auth.desktop.kiro.dev/refreshToken", am.config.Region)
	if am.refreshURL != "" {
		refreshURL = am.refreshURL
	}

	payload := map[string]string{
		"refreshToken": am.refreshToken,
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal refresh payload: %w", err)
	}

	req, err := http.NewRequestWithContext(context.TODO(), "POST", refreshURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token refresh failed with status: %d, body: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode refresh response: %w", err)
	}

	accessToken, ok := result["accessToken"].(string)
	if !ok || accessToken == "" {
		return fmt.Errorf("response does not contain accessToken")
	}

	am.accessToken = accessToken
	if refreshToken, ok := result["refreshToken"].(string); ok && refreshToken != "" {
		am.refreshToken = refreshToken
	}
	if expiresIn, ok := result["expiresIn"].(float64); ok && expiresIn > 0 {
		am.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second).Add(-60 * time.Second)
	}
	if profileArn, ok := result["profileArn"].(string); ok && profileArn != "" {
		am.profileArn = profileArn
	}

	log.Printf("Token refreshed via Kiro Desktop Auth, expires: %v", am.expiresAt)

	return nil
}

func (am *AuthManager) refreshTokenAWSSSO() error {
	if am.refreshToken == "" {
		return fmt.Errorf("refresh token is not set")
	}
	if am.clientID == "" {
		return fmt.Errorf("client ID is not set (required for AWS SSO)")
	}
	if am.clientSecret == "" {
		return fmt.Errorf("client secret is not set (required for AWS SSO)")
	}

	ssoRegion := am.ssoRegion
	if ssoRegion == "" {
		ssoRegion = am.config.Region
	}

	refreshURL := fmt.Sprintf("https://oidc.%s.amazonaws.com/token", ssoRegion)
	if am.refreshURL != "" {
		refreshURL = am.refreshURL
	}

	payload := map[string]string{
		"grantType":    "refresh_token",
		"clientId":     am.clientID,
		"clientSecret": am.clientSecret,
		"refreshToken": am.refreshToken,
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal refresh payload: %w", err)
	}

	req, err := http.NewRequestWithContext(context.TODO(), "POST", refreshURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	clientIDLog := am.clientID
	if len(clientIDLog) > 8 {
		clientIDLog = clientIDLog[:8]
	}
	log.Printf("Refreshing token via AWS SSO OIDC: url=%s, clientId=%s...", refreshURL, clientIDLog)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to refresh AWS SSO token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("AWS SSO token refresh failed with status: %d, body: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode refresh response: %w", err)
	}

	accessToken, ok := result["accessToken"].(string)
	if !ok || accessToken == "" {
		return fmt.Errorf("response does not contain accessToken: %+v", result)
	}

	am.accessToken = accessToken
	if refreshToken, ok := result["refreshToken"].(string); ok && refreshToken != "" {
		am.refreshToken = refreshToken
	}
	if expiresIn, ok := result["expiresIn"].(float64); ok && expiresIn > 0 {
		am.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second).Add(-60 * time.Second)
	}

	log.Printf("Token refreshed via AWS SSO OIDC, expires: %v", am.expiresAt)

	return nil
}

func (am *AuthManager) ForceRefresh() (string, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if err := am.refreshTokenLocked(); err != nil {
		return "", err
	}

	return am.accessToken, nil
}

func (am *AuthManager) APIHost() string {
	return fmt.Sprintf("https://q.%s.amazonaws.com", am.config.Region)
}

func (am *AuthManager) ProfileArn() string {
	return am.profileArn
}

func (am *AuthManager) Region() string {
	return am.config.Region
}

func (am *AuthManager) AuthType() AuthType {
	return am.authType
}
