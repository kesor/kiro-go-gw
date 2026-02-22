package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"kiro-go-gw/internal/auth"
	"kiro-go-gw/internal/client"
	"kiro-go-gw/internal/config"
	"kiro-go-gw/internal/converter"
	"kiro-go-gw/internal/debug"
	"kiro-go-gw/internal/models"
	"kiro-go-gw/internal/streaming"
)

type Server struct {
	cfg        *config.Config
	authMgr    *auth.AuthManager
	kiroClient *client.KiroClient
	mux        *http.ServeMux
}

func New(cfg *config.Config) (*Server, error) {
	// Validate config
	if cfg.ProxyAPIKey == "" {
		return nil, fmt.Errorf("PROXY_API_KEY is required")
	}

	// Check for credentials
	hasCreds := cfg.RefreshToken != "" || cfg.CredsFile != "" || cfg.CliDbFile != ""
	if !hasCreds {
		return nil, fmt.Errorf("no Kiro credentials configured. Set one of: REFRESH_TOKEN, KIRO_CREDS_FILE, or KIRO_CLI_DB_FILE")
	}

	// Create auth manager
	authCfg := &auth.AuthConfig{
		RefreshToken: cfg.RefreshToken,
		ProfileArn:   cfg.ProfileArn,
		Region:       cfg.Region,
		CredsFile:    cfg.CredsFile,
		CliDbFile:    cfg.CliDbFile,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
	}

	authMgr, err := auth.NewAuthManager(authCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth manager: %w", err)
	}

	// Create HTTP client
	kiroClient := client.NewKiroClient(authMgr)

	s := &Server{
		cfg:        cfg,
		authMgr:    authMgr,
		kiroClient: kiroClient,
		mux:        http.NewServeMux(),
	}

	s.registerRoutes()
	return s, nil
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/", s.handleRoot)
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/v1/models", s.handleModels)
	s.mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Kiro Gateway is running",
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := s.verifyAPIKey(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		debug.LogRequest(debug.EventSourceGateway, r.Method, r.URL.String(), r.Header, nil, 0)
		debug.LogResponse(debug.EventSourceGateway, r.Method, r.URL.String(), http.StatusUnauthorized, nil, []byte(err.Error()), time.Since(startTime).Milliseconds())
		return
	}

	profileArn := s.authMgr.ProfileArn()

	kiroModels, err := s.kiroClient.ListAvailableModels(r.Context(), profileArn)
	if err != nil {
		log.Printf("Failed to fetch models from Kiro API: %v", err)
		http.Error(w, fmt.Sprintf("Failed to fetch models: %v", err), http.StatusBadGateway)
		debug.LogResponse(debug.EventSourceGateway, r.Method, r.URL.String(), http.StatusBadGateway, nil, []byte(err.Error()), time.Since(startTime).Milliseconds())
		return
	}

	modelList := make([]models.Model, 0, len(kiroModels.Models))
	for _, m := range kiroModels.Models {
		maxInputTokens := 0
		if m.TokenLimits != nil {
			maxInputTokens = m.TokenLimits.MaxInputTokens
		}
		supportsPromptCaching := false
		if m.PromptCaching != nil {
			supportsPromptCaching = m.PromptCaching.SupportsPromptCaching
		}
		ownedBy := deriveProvider(m.ModelID)
		modelList = append(modelList, models.Model{
			ID:                    m.ModelID,
			Object:                "model",
			Created:               0,
			OwnedBy:               ownedBy,
			Permission:            nil,
			Description:           m.Description,
			RateMultiplier:        m.RateMultiplier,
			RateUnit:              m.RateUnit,
			MaxInputTokens:        maxInputTokens,
			SupportsPromptCaching: supportsPromptCaching,
		})
	}

	resp := models.ModelList{
		Object: "list",
		Data:   modelList,
	}

	respBytes, _ := json.Marshal(resp)
	debug.LogRequest(debug.EventSourceGateway, r.Method, r.URL.String(), r.Header, nil, time.Since(startTime).Milliseconds())
	debug.LogResponse(debug.EventSourceGateway, r.Method, r.URL.String(), http.StatusOK, w.Header(), respBytes, time.Since(startTime).Milliseconds())

	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify API key
	if err := s.verifyAPIKey(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		debug.LogRequest(debug.EventSourceGateway, r.Method, r.URL.String(), r.Header, nil, 0)
		debug.LogResponse(debug.EventSourceGateway, r.Method, r.URL.String(), http.StatusUnauthorized, nil, []byte(err.Error()), time.Since(startTime).Milliseconds())
		return
	}

	// Parse request
	var req models.ChatCompletionRequest
	maxBodySize := int64(10 * 1024 * 1024) // 10MB limit
	bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodySize))
	if err != nil {
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		debug.LogRequest(debug.EventSourceGateway, r.Method, r.URL.String(), r.Header, bodyBytes, time.Since(startTime).Milliseconds())
		debug.LogResponse(debug.EventSourceGateway, r.Method, r.URL.String(), http.StatusBadRequest, nil, []byte(err.Error()), time.Since(startTime).Milliseconds())
		return
	}

	// Generate conversation ID
	conversationID := generateConversationID()

	// Build Kiro payload
	payload, err := converter.BuildKiroPayload(&req, conversationID, s.authMgr.ProfileArn())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to build payload: %v", err), http.StatusBadRequest)
		debug.LogRequest(debug.EventSourceGateway, r.Method, r.URL.String(), r.Header, bodyBytes, time.Since(startTime).Milliseconds())
		debug.LogResponse(debug.EventSourceGateway, r.Method, r.URL.String(), http.StatusBadRequest, nil, []byte(err.Error()), time.Since(startTime).Milliseconds())
		return
	}

	// Make request to Kiro API
	url := s.authMgr.APIHost() + "/generateAssistantResponse"

	debug.LogRequest(debug.EventSourceGateway, r.Method, r.URL.String(), r.Header, bodyBytes, time.Since(startTime).Milliseconds())

	if req.Stream {
		s.handleStreaming(w, r, url, payload, req.Model, startTime)
	} else {
		s.handleNonStreaming(w, r, url, payload, req.Model, startTime)
	}
}

func (s *Server) handleStreaming(w http.ResponseWriter, r *http.Request, url string, payload map[string]interface{}, model string, startTime time.Time) {
	resp, err := s.kiroClient.DoRequest(r.Context(), "POST", url, payload, true)
	if err != nil {
		http.Error(w, fmt.Sprintf("Request failed: %v", err), http.StatusBadGateway)
		debug.LogResponse(debug.EventSourceGateway, r.Method, r.URL.String(), http.StatusBadGateway, nil, []byte(err.Error()), time.Since(startTime).Milliseconds())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("Kiro API error: %s", body), resp.StatusCode)
		debug.LogResponse(debug.EventSourceGateway, r.Method, r.URL.String(), resp.StatusCode, resp.Header, body, time.Since(startTime).Milliseconds())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Stream conversion
	ch, err := streaming.StreamToOpenAI(resp.Body, model)
	if err != nil {
		log.Printf("Stream error: %v", err)
		return
	}

	for data := range ch {
		fmt.Fprint(w, data)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	debug.LogResponse(debug.EventSourceGateway, r.Method, r.URL.String(), http.StatusOK, w.Header(), []byte("[streaming]"), time.Since(startTime).Milliseconds())
}

func (s *Server) handleNonStreaming(w http.ResponseWriter, r *http.Request, url string, payload map[string]interface{}, model string, startTime time.Time) {
	resp, err := s.kiroClient.DoRequest(r.Context(), "POST", url, payload, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Request failed: %v", err), http.StatusBadGateway)
		debug.LogResponse(debug.EventSourceGateway, r.Method, r.URL.String(), http.StatusBadGateway, nil, []byte(err.Error()), time.Since(startTime).Milliseconds())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("Kiro API error: %s", body), resp.StatusCode)
		debug.LogResponse(debug.EventSourceGateway, r.Method, r.URL.String(), resp.StatusCode, resp.Header, body, time.Since(startTime).Milliseconds())
		return
	}

	// Collect and convert response
	openaiResp, err := streaming.CollectResponse(resp.Body, model)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse response: %v", err), http.StatusInternalServerError)
		debug.LogResponse(debug.EventSourceGateway, r.Method, r.URL.String(), http.StatusInternalServerError, nil, []byte(err.Error()), time.Since(startTime).Milliseconds())
		return
	}

	respBytes, _ := json.Marshal(openaiResp)
	debug.LogResponse(debug.EventSourceGateway, r.Method, r.URL.String(), http.StatusOK, w.Header(), respBytes, time.Since(startTime).Milliseconds())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(openaiResp)
}

func (s *Server) verifyAPIKey(r *http.Request) error {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return fmt.Errorf("missing authorization header")
	}

	// Support Bearer token
	if strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimPrefix(auth, "Bearer ")
		if token == s.cfg.ProxyAPIKey {
			return nil
		}
	}

	return fmt.Errorf("invalid API key")
}

func generateConversationID() string {
	return fmt.Sprintf("conv-%d-%s", time.Now().Unix(), randomHex(8))
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based if crypto rand fails
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)[:n]
}

func (s *Server) Start(addr string) error {
	log.Printf("Starting Kiro Gateway on %s", addr)
	return http.ListenAndServe(addr, s)
}

func Run() {
	// Load config
	cfg := config.Load()

	// Create server
	srv, err := New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	addr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort)

	// Create HTTP server
	httpServer := &http.Server{
		Addr:    addr,
		Handler: srv,
	}

	// Start server in goroutine
	go func() {
		log.Printf("Starting Kiro Gateway on %s", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	debug.Close()
	log.Println("Server exited")
}

func deriveProvider(modelID string) string {
	switch {
	case strings.HasPrefix(modelID, "claude-"):
		return "anthropic"
	case strings.HasPrefix(modelID, "deepseek-"):
		return "deepseek"
	case strings.HasPrefix(modelID, "qwen-"):
		return "qwen"
	case strings.HasPrefix(modelID, "mini-max-"):
		return "minimax"
	case modelID == "auto":
		return "kiro"
	default:
		return "kiro"
	}
}
