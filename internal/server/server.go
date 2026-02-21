package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"kiro-go-gw/internal/auth"
	"kiro-go-gw/internal/client"
	"kiro-go-gw/internal/config"
	"kiro-go-gw/internal/converter"
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
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Simplified model list - in production would fetch from Kiro API
	modelList := []models.Model{
		{ID: "auto", Object: "model", OwnedBy: "kiro"},
		{ID: "claude-sonnet-4.5", Object: "model", OwnedBy: "anthropic"},
		{ID: "claude-haiku-4.5", Object: "model", OwnedBy: "anthropic"},
		{ID: "claude-opus-4.5", Object: "model", OwnedBy: "anthropic"},
		{ID: "claude-sonnet-4", Object: "model", OwnedBy: "anthropic"},
		{ID: "deepseek-v3.2", Object: "model", OwnedBy: "deepseek"},
		{ID: "mini-max-m2.1", Object: "model", OwnedBy: "minimax"},
		{ID: "qwen3-coder-next", Object: "model", OwnedBy: "qwen"},
	}

	json.NewEncoder(w).Encode(models.ModelList{
		Object: "list",
		Data:   modelList,
	})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify API key
	if err := s.verifyAPIKey(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Parse request
	var req models.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Generate conversation ID
	conversationID := generateConversationID()

	// Build Kiro payload
	payload, err := converter.BuildKiroPayload(&req, conversationID, s.authMgr.ProfileArn())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to build payload: %v", err), http.StatusBadRequest)
		return
	}

	// Make request to Kiro API
	url := s.authMgr.APIHost() + "/generateAssistantResponse"

	if req.Stream {
		s.handleStreaming(w, r, url, payload, req.Model)
	} else {
		s.handleNonStreaming(w, r, url, payload, req.Model)
	}
}

func (s *Server) handleStreaming(w http.ResponseWriter, r *http.Request, url string, payload map[string]interface{}, model string) {
	resp, err := s.kiroClient.DoRequest(r.Context(), "POST", url, payload, true)
	if err != nil {
		http.Error(w, fmt.Sprintf("Request failed: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("Kiro API error: %s", body), resp.StatusCode)
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
}

func (s *Server) handleNonStreaming(w http.ResponseWriter, r *http.Request, url string, payload map[string]interface{}, model string) {
	resp, err := s.kiroClient.DoRequest(r.Context(), "POST", url, payload, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Request failed: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("Kiro API error: %s", body), resp.StatusCode)
		return
	}

	// Collect and convert response
	openaiResp, err := streaming.CollectResponse(resp.Body, model)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse response: %v", err), http.StatusInternalServerError)
		return
	}

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
	result := make([]byte, n)
	for i := range result {
		result[i] = "0123456789abcdef"[i%16]
	}
	return string(result)
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

	// Start server
	addr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort)
	if err := srv.Start(addr); err != nil {
		log.Fatal(err)
	}
}
