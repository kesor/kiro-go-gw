// Package debug provides debug logging functionality for the Kiro gateway.
package debug

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const maxBodyLength = 10 * 1024

type DebugLogger struct {
	logger *log.Logger
	file   *os.File
	mu     sync.Mutex
}

var (
	defaultLogger *DebugLogger
	initOnce      sync.Once
)

type EventType string

const (
	EventTypeRequest  EventType = "request"
	EventTypeResponse EventType = "response"
	EventTypeAuth     EventType = "auth"
)

type EventSource string

const (
	EventSourceGateway EventSource = "gateway"
	EventSourceAmazonQ EventSource = "amazon_q"
	EventSourceAuth    EventSource = "auth"
)

type Event struct {
	Timestamp string            `json:"timestamp"`
	Type      EventType         `json:"type"`
	Source    EventSource       `json:"source"`
	Method    string            `json:"method,omitempty"`
	URL       string            `json:"url,omitempty"`
	Status    int               `json:"status,omitempty"`
	Duration  int64             `json:"duration_ms,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      string            `json:"body,omitempty"`
	Message   string            `json:"message,omitempty"`
	Error     string            `json:"error,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}

//go:noinline
func Init(debug bool, logFile string) bool {
	initOnce.Do(func() {
		if debug {
			defaultLogger = &DebugLogger{}

			var output io.Writer = os.Stdout
			if logFile != "" {
				f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
				if err != nil {
					log.Printf("Failed to open debug log file: %v, falling back to stdout", err)
				} else {
					output = f
					defaultLogger.file = f
				}
			}

			defaultLogger.logger = log.New(output, "", 0)
		} else {
			defaultLogger = &DebugLogger{}
			defaultLogger.logger = log.New(io.Discard, "", 0)
		}
	})
	return debug
}

func Close() {
	if defaultLogger != nil && defaultLogger.file != nil {
		defaultLogger.file.Close()
	}
}

func Enabled() bool {
	return defaultLogger != nil && defaultLogger.logger != nil && defaultLogger.logger.Writer() != io.Discard
}

func LogRequest(source EventSource, method, url string, headers http.Header, body []byte, duration int64) {
	if !Enabled() {
		return
	}

	e := Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Type:      EventTypeRequest,
		Source:    source,
		Method:    method,
		URL:       url,
		Headers:   redactedHeaders(headers),
		Body:      truncate(string(body)),
		Duration:  duration,
	}
	defaultLogger.log(e)
}

func LogResponse(source EventSource, method, url string, status int, headers http.Header, body []byte, duration int64) {
	if !Enabled() {
		return
	}

	e := Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Type:      EventTypeResponse,
		Source:    source,
		Method:    method,
		URL:       url,
		Status:    status,
		Headers:   redactedHeaders(headers),
		Body:      truncate(string(body)),
		Duration:  duration,
	}
	defaultLogger.log(e)
}

func LogAuth(event, message, errorMsg string, meta map[string]string) {
	if !Enabled() {
		return
	}

	e := Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Type:      EventTypeAuth,
		Source:    EventSourceAuth,
		Message:   message,
		Error:     errorMsg,
		Meta:      meta,
	}
	if event != "" {
		if e.Meta == nil {
			e.Meta = make(map[string]string)
		}
		e.Meta["event"] = event
	}
	defaultLogger.log(e)
}

func (d *DebugLogger) log(e Event) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := json.Marshal(e)
	if err != nil {
		log.Printf("Failed to marshal debug event: %v", err)
		return
	}
	d.logger.Println(string(data))
}

func redactedHeaders(headers http.Header) map[string]string {
	if headers == nil {
		return nil
	}

	sensitiveHeaders := map[string]bool{
		"Authorization":       true,
		"Cookie":              true,
		"X-Api-Key":           true,
		"X-Auth-Token":        true,
		"Proxy-Authorization": true,
		"Set-Cookie":          true,
	}

	result := make(map[string]string)
	for k, v := range headers {
		value := strings.Join(v, ", ")
		if sensitiveHeaders[http.CanonicalHeaderKey(k)] {
			value = redactToken(value)
		}
		result[k] = value
	}
	return result
}

func redactToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return "[REDACTED]"
	}

	lower := strings.ToLower(token)
	if strings.HasPrefix(lower, "bearer ") || strings.HasPrefix(lower, "basic ") {
		parts := strings.SplitN(token, " ", 2)
		if len(parts) == 2 {
			return parts[0] + " " + redactSecret(parts[1])
		}
	}

	return redactSecret(token)
}

func redactSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "[REDACTED]"
	}

	if len(secret) <= 8 {
		return "[REDACTED]"
	}

	const prefixLen = 4
	const suffixLen = 4
	return fmt.Sprintf("%s...%s", secret[:prefixLen], secret[len(secret)-suffixLen:])
}

func truncate(s string) string {
	if len(s) <= maxBodyLength {
		return s
	}
	return s[:maxBodyLength] + "\n...[truncated]"
}
