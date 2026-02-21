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
		if payload != nil {
			jsonData, err := json.Marshal(payload)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal payload: %w", err)
			}
			body = bytes.NewBuffer(jsonData)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, body)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "aws-sdk-js/1.0.27 ua/2.1 os/linux lang/go")
		req.Header.Set("x-amz-user-agent", "aws-sdk-js/1.0.27 KiroGateway/1.0")
		req.Header.Set("x-amzn-codewhisperer-optout", "true")
		req.Header.Set("x-amzn-kiro-agent-mode", "vibe")

		// Use streaming client for streaming requests
		client := c.httpClient
		if stream {
			client = &http.Client{
				Timeout: StreamingTimeout,
			}
			req.Header.Set("Connection", "close")
		}

		resp, err := client.Do(req)
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
