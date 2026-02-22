package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"kiro-go-gw/internal/auth"
	"kiro-go-gw/internal/debug"
)

const (
	MaxRetries       = 3
	BaseRetryDelay   = 1.0
	StreamingTimeout = 300 * time.Second
	RequestTimeout   = 30 * time.Second
)

type KiroClient struct {
	authManager auth.AuthProvider
	httpClient  *http.Client
}

func NewKiroClient(authManager auth.AuthProvider) *KiroClient {
	return &KiroClient{
		authManager: authManager,
		httpClient: &http.Client{
			Timeout: RequestTimeout,
		},
	}
}

func (c *KiroClient) DoRequest(ctx context.Context, method, url string, payload interface{}, stream bool) (*http.Response, error) {
	maxRetries := MaxRetries
	if stream {
		maxRetries = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		// Get token
		token, err := c.authManager.GetAccessToken()
		if err != nil {
			return nil, fmt.Errorf("failed to get access token: %w", err)
		}

		// Build request
		var body io.Reader
		var bodyBytes []byte
		if payload != nil {
			jsonData, err := json.Marshal(payload)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal payload: %w", err)
			}
			bodyBytes = jsonData
			body = bytes.NewBuffer(jsonData)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, body)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/x-amz-json-1.0")
		req.Header.Set("User-Agent", "aws-sdk-rust/1.3.11 ua/2.1 api/codewhispererstreaming/0.1.13922 os/linux lang/rust/1.92.0 md/appVersion-1.25.1 app/AmazonQ-For-CLI")
		req.Header.Set("x-amz-user-agent", "aws-sdk-rust/1.3.11 ua/2.1 api/codewhispererstreaming/0.1.13922 os/linux lang/rust/1.92.0 m/F app/AmazonQ-For-CLI")
		req.Header.Set("x-amzn-codewhisperer-optout", "false")
		req.Header.Set("x-amz-target", "AmazonCodeWhispererStreamingService.GenerateAssistantResponse")

		startTime := time.Now()

		// Use streaming client for streaming requests
		client := c.httpClient
		if stream {
			client = &http.Client{
				Timeout: StreamingTimeout,
			}
			req.Header.Set("Connection", "close")
		}

		resp, err := client.Do(req)
		duration := time.Since(startTime).Milliseconds()

		debug.LogRequest(debug.EventSourceAmazonQ, method, url, req.Header, bodyBytes, duration)

		if err != nil {
			debug.LogResponse(debug.EventSourceAmazonQ, method, url, 0, nil, []byte(err.Error()), duration)
		} else if stream {
			debug.LogResponse(debug.EventSourceAmazonQ, method, url, resp.StatusCode, resp.Header, []byte("[streaming]"), duration)
		} else if debug.Enabled() {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body = io.NopCloser(bytes.NewBuffer(respBody))
			debug.LogResponse(debug.EventSourceAmazonQ, method, url, resp.StatusCode, resp.Header, respBody, duration)
		}

		if err != nil {
			lastErr = err
			// Check if retryable
			if isRetryableError(err) && attempt < maxRetries-1 {
				delay := BaseRetryDelay * math.Pow(2, float64(attempt))
				time.Sleep(time.Duration(delay * float64(time.Second)))
				continue
			}
			return nil, fmt.Errorf("request failed: %w", err)
		}

		// Check status code
		switch {
		case resp.StatusCode == http.StatusOK:
			return resp, nil
		case resp.StatusCode == http.StatusForbidden:
			// Token expired, refresh and retry
			resp.Body.Close()
			if _, err := c.authManager.ForceRefresh(); err != nil {
				log.Printf("Token refresh failed: %v", err)
				return nil, fmt.Errorf("authentication failed")
			}
			continue
		case resp.StatusCode == http.StatusTooManyRequests:
			// Rate limited, backoff and retry
			resp.Body.Close()
			delay := BaseRetryDelay * math.Pow(2, float64(attempt))
			time.Sleep(time.Duration(delay * float64(time.Second)))
			continue
		case resp.StatusCode >= 500:
			// Server error, retry
			resp.Body.Close()
			delay := BaseRetryDelay * math.Pow(2, float64(attempt))
			time.Sleep(time.Duration(delay * float64(time.Second)))
			continue
		default:
			// Other error, return as is
			return resp, nil
		}
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", maxRetries, lastErr)
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	// Only retry deadline exceeded, not canceled (user explicitly canceled)
	if err == context.DeadlineExceeded {
		return true
	}
	// Check for timeout errors
	errStr := err.Error()
	timeoutErrors := []string{"timeout", "timed out", "no such host", "connection refused"}
	for _, t := range timeoutErrors {
		if strings.Contains(errStr, t) {
			return true
		}
	}
	return false
}

func (c *KiroClient) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

type ListModelsResponse struct {
	DefaultModel *ModelInfo  `json:"defaultModel"`
	Models       []ModelInfo `json:"models"`
}

type ModelInfo struct {
	Type                string         `json:"__type"`
	ModelID             string         `json:"modelId"`
	ModelName           string         `json:"modelName"`
	Description         string         `json:"description"`
	PromptCaching       *PromptCaching `json:"promptCaching"`
	RateMultiplier      float64        `json:"rateMultiplier"`
	RateUnit            string         `json:"rateUnit"`
	SupportedInputTypes []string       `json:"supportedInputTypes"`
	TokenLimits         *TokenLimits   `json:"tokenLimits"`
}

type PromptCaching struct {
	MaximumCacheCheckpointsPerRequest int  `json:"maximumCacheCheckpointsPerRequest"`
	MinimumTokensPerCacheCheckpoint   int  `json:"minimumTokensPerCacheCheckpoint"`
	SupportsPromptCaching             bool `json:"supportsPromptCaching"`
}

type TokenLimits struct {
	MaxInputTokens int `json:"maxInputTokens"`
}

func (c *KiroClient) ListAvailableModels(ctx context.Context, profileArn string) (*ListModelsResponse, error) {
	apiHost := c.authManager.APIHost()
	origin := "KIRO_CLI"
	endpoint := fmt.Sprintf("%s/", apiHost)

	payload := map[string]string{
		"origin":     origin,
		"profileArn": profileArn,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	token, err := c.authManager.GetAccessToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("x-amz-target", "AmazonCodeWhispererService.ListAvailableModels")
	req.Header.Set("User-Agent", "aws-sdk-rust/1.3.11 ua/2.1 api/codewhispererruntime/0.1.13922 os/linux lang/rust/1.92.0 md/appVersion-1.25.1 app/AmazonQ-For-CLI")
	req.Header.Set("x-amz-user-agent", "aws-sdk-rust/1.3.11 ua/2.1 api/codewhispererruntime/0.1.13922 os/linux lang/rust/1.92.0 m/F,C app/AmazonQ-For-CLI")
	req.Header.Set("x-amzn-codewhisperer-optout", "false")

	startTime := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(startTime).Milliseconds()

	debug.LogRequest(debug.EventSourceAmazonQ, "POST", endpoint, req.Header, jsonData, duration)

	if err != nil {
		debug.LogResponse(debug.EventSourceAmazonQ, "POST", endpoint, 0, nil, []byte(err.Error()), duration)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if debug.Enabled() {
			body, _ := io.ReadAll(resp.Body)
			debug.LogResponse(debug.EventSourceAmazonQ, "POST", endpoint, resp.StatusCode, resp.Header, body, duration)
		}
		return nil, fmt.Errorf("ListAvailableModels failed: status %d", resp.StatusCode)
	}

	if debug.Enabled() {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body = io.NopCloser(bytes.NewBuffer(respBody))
		debug.LogResponse(debug.EventSourceAmazonQ, "POST", endpoint, resp.StatusCode, resp.Header, respBody, duration)
	}

	var result ListModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}
