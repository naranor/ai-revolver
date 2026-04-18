// Package main is the entry point for the AI Proxy service
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"ai-proxy/config"
	"ai-proxy/db"
	"ai-proxy/logger"
	"ai-proxy/proxy"
)

const maxRequestBodySize = 10 << 20 // 10MB

var (
	debugMode bool
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

func main() {
	proxy.FindProviderFunc = FindProvider
	proxy.BuildModelsEndpointFunc = BuildModelsEndpoint
	proxy.FetchFromProviderFunc = FetchFromProvider
	proxy.WriteOllamaModelsResponseFunc = WriteOllamaModelsResponse

	port := flag.Int("port", 8081, "Port to listen on")
	showVersion := flag.Bool("version", false, "Show version information")
	latencyThreshold := flag.Int64("latency-threshold", 10000, "Latency threshold in ms to mark model as slow (0 to disable)")
	blockDuration := flag.Int64("block-duration", 5, "Duration in minutes to block inactive models (0 to disable)")
	maxIdleConns := flag.Int("max-idle-conns", 100, "Maximum idle connections")
	maxIdleConnsPerHost := flag.Int("max-idle-conns-per-host", 20, "Maximum idle connections per host")
	idleConnTimeout := flag.Duration("idle-conn-timeout", 90*time.Second, "Idle connection timeout")
	streamBufferSize := flag.Int64("stream-buffer-size", 2*1024*1024, "Stream buffer size in bytes for both Ollama and OpenAI responses")
	configPath := flag.String("config", "data/config.json", "Path to config file")
	statsPath := flag.String("stats", "data/stats.db", "Path to stats database")
	debug := flag.Bool("debug", false, "Enable debug logging for requests")
	trace := flag.Bool("trace", false, "Enable trace logging for all payloads and responses")
	flag.Parse()

	if *showVersion {
		fmt.Printf("AI Proxy Version: %s\n", version)
		fmt.Printf("Commit: %s\n", commit)
		fmt.Printf("Build Time: %s\n", buildTime)
		os.Exit(0)
	}

	debugMode = *debug
	logger.Init(debugMode)
	proxy.TraceMode = *trace

	proxy.SetLatencyThreshold(*latencyThreshold)
	proxy.SetBlockDuration(time.Duration(*blockDuration) * time.Minute)

	// Initialize configurable buffer sizes for streaming responses
	proxy.OllamaStreamBufferSize = *streamBufferSize
	proxy.OpenAIStreamBufferSize = *streamBufferSize

	logger.Info().
		Int("port", *port).
		Int64("latency_threshold_ms", *latencyThreshold).
		Int64("block_duration_min", *blockDuration).
		Str("config", *configPath).
		Str("stats", *statsPath).
		Bool("debug", debugMode).
		Msg("Starting AI Revolver")

	if err := ensureDir(filepath.Dir(*configPath)); err != nil {
		logger.Fatal().Err(err).Msg("Failed to create config directory")
	}

	if err := db.InitDB(*statsPath); err != nil {
		logger.Fatal().Err(err).Msg("Failed to init DB")
	}

	if err := config.LoadConfig(*configPath); err != nil {
		logger.Fatal().Err(err).Msg("Failed to load config")
	}

	// Initialize HTTP clients with configurable connection pool settings and split timeouts
	cfg := config.GetConfig()
	proxy.InitHTTPClients(*maxIdleConns, *maxIdleConnsPerHost, *idleConnTimeout, cfg.GetConnectTimeout(), cfg.GetResponseTimeout())

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	// Initialize WarmupManager
	wm := proxy.NewWarmupManager()
	go wm.Start(appCtx)

	startBackgroundTasks()
	setupSignalHandler(configPath)
	setupRoutes()

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", *port),
		ReadTimeout:       0, // Allow large request bodies without timing out
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		appCancel()
		logger.Info().Msg("Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Error().Err(err).Msg("Server shutdown error")
		}
	}()

	logger.Info().Msg("AI Proxy Service started")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatal().Err(err).Msg("Server failed")
	}

	// Final cleanup
	_ = db.Close()
	logger.Shutdown()
}

func ensureDir(dir string) error {
	if dir == "." || dir == "" {
		return nil
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0700)
	}
	return nil
}

func startBackgroundTasks() {
	go startTicker(60*time.Second, func() {
		if err := db.RecordStatsHistory(); err != nil {
			logger.Error().Err(err).Msg("Failed to record stats history")
		}
	})

	go startTicker(60*time.Second, func() {
		proxy.CleanupOldEntries()
	})
}

func startTicker(interval time.Duration, fn func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		fn()
	}
}

func setupSignalHandler(configPath *string) {
	sighupChan := make(chan os.Signal, 1)
	signal.Notify(sighupChan, syscall.SIGHUP)
	go func() {
		for range sighupChan {
			logger.Info().Msg("Reloading config...")
			if err := config.ReloadConfig(*configPath); err != nil {
				logger.Error().Err(err).Msg("Failed to reload config")
			}
		}
	}()
}

func setupRoutes() {
	http.HandleFunc("/health", withCORS(withLogging(healthHandler)))
	http.HandleFunc("/api/v1/stats", withCORS(withLogging(statsHandler)))
	http.HandleFunc("/api/v1/stats/history", withCORS(withLogging(statsHistoryHandler)))
	http.HandleFunc("/api/v1/chat/completions", withCORS(withLogging(chatCompletionsHandler)))
	http.HandleFunc("/api/v1/test", withCORS(withLogging(testHandler)))
	http.HandleFunc("/api/v1/config", withCORS(withLogging(configHandler)))
	http.HandleFunc("/api/v1/active-model", withCORS(withLogging(activeModelHandler)))
	http.HandleFunc("/api/v1/scout", withCORS(withLogging(scoutHandler)))
	http.HandleFunc("/api/v1/modelinfo", withCORS(withLogging(modelInfoHandler)))
	http.HandleFunc("/api/v1/models", withCORS(withLogging(modelsHandler)))
	// Streamable HTTP transport for MCP
	http.HandleFunc("/mcp", withCORS(withLogging(proxy.HandleMCPEndpoint)))
}

// OpenAIModel represents the structure for a single model in the OpenAI API format.
type OpenAIModel struct {
	Capabilities map[string]interface{} `json:"capabilities"`
	Parent       *string                `json:"parent"`
	ID           string                 `json:"id"`
	Object       string                 `json:"object"`
	OwnedBy      string                 `json:"owned_by"`
	Root         string                 `json:"root"`
	Permission   []string               `json:"permission"`
	Created      int64                  `json:"created"`
}

// OpenAIModelsList represents the response structure for the models list endpoint.
type OpenAIModelsList struct {
	Object string        `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

// modelsHandler returns a list of all models in the OpenAI API format.
func modelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := config.GetConfig()
	models := []OpenAIModel{
		{
			ID:      "auto",
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: "system",
			Root:    "auto",
			Parent:  nil,
			Capabilities: map[string]interface{}{
				"description": "Automatically selects the best available provider and model based on performance and availability.",
			},
		},
	}

	for _, provider := range cfg.Providers {
		for _, model := range provider.Models {
			models = append(models, OpenAIModel{
				ID:      fmt.Sprintf("%s/%s", provider.Name, model.Name),
				Object:  "model",
				Created: time.Now().Unix(),
				OwnedBy: provider.Name,
				Root:    model.Name,
				Parent:  nil,
				Capabilities: map[string]interface{}{
					"thinking":   model.Thinking,
					"reasoning":  model.Reasoning,
					"tools":      model.Tools,
					"max_tokens": model.MaxTokens,
				},
			})
		}
	}

	response := OpenAIModelsList{
		Object: "list",
		Data:   models,
	}

	WriteJSON(w, response)
}

func withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Session-Id, Mcp-Protocol-Version")
		w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

func withLogging(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if debugMode {
			logger.Debug().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("remote", r.RemoteAddr).
				Msg("Request")
		}
		next(w, r)
	}
}

// healthHandler handles health check requests
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("OK")); err != nil {
		logger.Error().Err(err).Msg("Failed to write health response")
	}
}

// statsHandler returns statistics
func statsHandler(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	stats, err := db.GetStatsWithPeriod(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	blockedModels := proxy.GetBlockedModels()
	latencyThreshold := proxy.GetLatencyThreshold()
	blockDuration := proxy.GetBlockDuration()

	if models, ok := stats["models"].([]map[string]interface{}); ok {
		for _, model := range models {
			provider, _ := model["provider"].(string)
			modelName, _ := model["model"].(string)
			key := provider + ":" + modelName
			if blockedInfo, exists := blockedModels[key]; exists {
				model["is_blocked"] = true
				model["is_slow"] = blockedInfo["is_slow"]
				model["blocked_latency"] = blockedInfo["latency"]
				model["last_code"] = blockedInfo["last_code"]
			} else {
				model["is_blocked"] = false
				model["is_slow"] = false
			}
		}
	}

	stats["latency_threshold_ms"] = latencyThreshold
	stats["block_duration_sec"] = int(blockDuration.Seconds())
	WriteJSON(w, stats)
}

// statsHistoryHandler returns statistics history
func statsHistoryHandler(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	history, err := db.GetStatsHistoryWithPeriod(60, period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	WriteJSON(w, history)
}

// chatCompletionsHandler handles chat completion requests
func chatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req, err := readProxyRequestFunc(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer proxy.RequestPool.Put(req)

	logger.Debug().
		Str("model", req.Model).
		Bool("stream", req.Stream).
		Msg("Processing request")

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		if streamErr := proxyStreamFunc(ctx, *req, w); streamErr != nil {
			logger.Error().Err(streamErr).Msg("Stream proxy failed")
			// Send error as SSE event
			errResp := map[string]interface{}{"error": streamErr.Error()}
			errJSON, _ := json.Marshal(errResp)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", errJSON)
		}
		// Always send DONE at the end
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	result, _, err := proxyProxyFunc(ctx, *req)
	if err != nil {
		logger.Error().Err(err).Msg("Proxy failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	WriteJSON(w, result)
}

// testHandler handles test/probe requests
func testHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req, err := readProxyRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer proxy.RequestPool.Put(req)

	req.Probe = true

	startTime := time.Now()
	result, code, err := proxyProxyFunc(ctx, *req)
	latency := time.Since(startTime).Milliseconds()

	WriteJSON(w, map[string]interface{}{
		"success":    err == nil,
		"latency_ms": latency,
		"response":   result,
		"error":      errorString(err),
		"last_code":  code,
	})
}

// configHandler handles config requests
func configHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		WriteJSON(w, config.GetConfig())
	case http.MethodPut:
		handleConfigUpdate(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	if err := config.UpdateConfig(body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Reset active model on config change
	proxy.ClearLastSuccessfulModel()
	proxy.ResetAllFailures()

	logger.Info().Msg("Config updated, active model reset")
	w.WriteHeader(http.StatusOK)
}

// activeModelHandler returns the last successful model
func activeModelHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	provider, model, latency := proxy.GetLastSuccessfulModel()
	WriteJSON(w, map[string]interface{}{
		"provider": provider,
		"model":    model,
		"latency":  latency,
		"active":   provider != "" && model != "",
	})
}

// scoutHandler returns available models from a provider
func scoutHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	providerName := r.URL.Query().Get("provider")
	target := FindProviderFunc(providerName)
	if target == nil {
		logger.Error().Str("provider", providerName).Msg("Scout: Provider not found")
		http.Error(w, "Provider not found", http.StatusNotFound)
		return
	}

	endpoint := BuildModelsEndpointFunc(target)
	body, err := FetchFromProviderFunc(ctx, target, endpoint)
	if err != nil {
		logger.Error().
			Err(err).
			Str("provider", providerName).
			Str("endpoint", endpoint).
			Msg("Scout: HTTP request failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if target.IsNativeOllama() {
		WriteOllamaModelsResponseFunc(w, body)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := io.Copy(w, body); err != nil {
		logger.Error().Err(err).Msg("Failed to copy response")
	}
	_ = body.Close()
}

// modelInfoHandler returns info about a specific model
func modelInfoHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	providerName := r.URL.Query().Get("provider")
	modelName := r.URL.Query().Get("model")

	if providerName == "" || modelName == "" {
		http.Error(w, "provider and model are required", http.StatusBadRequest)
		return
	}

	target := FindProviderFunc(providerName)
	if target == nil {
		http.Error(w, "Provider not found", http.StatusNotFound)
		return
	}

	endpoint := BuildModelsEndpointFunc(target)
	body, err := FetchFromProviderFunc(ctx, target, endpoint)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = body.Close() }()

	if target.IsNativeOllama() {
		FindOllamaModelFunc(w, body, modelName)
		return
	}

	FindOpenAIModelFunc(w, body, modelName)
}

// FindProviderFunc is a mockable version of FindProvider
var FindProviderFunc = FindProvider

// BuildModelsEndpointFunc is a mockable version of BuildModelsEndpoint
var BuildModelsEndpointFunc = BuildModelsEndpoint

// FetchFromProviderFunc is a mockable version of FetchFromProvider
var FetchFromProviderFunc = FetchFromProvider

// WriteOllamaModelsResponseFunc is a mockable version of WriteOllamaModelsResponse
var WriteOllamaModelsResponseFunc = WriteOllamaModelsResponse

// FindProvider returns a provider by name
func FindProvider(name string) *config.Provider {
	cfg := config.GetConfig()
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == name {
			return &cfg.Providers[i]
		}
	}
	return nil
}

// BuildModelsEndpoint returns the models endpoint for a provider
func BuildModelsEndpoint(p *config.Provider) string {
	baseURL := TrimTrailingSlash(p.BaseURL)
	if p.IsNativeOllama() {
		baseURL = TrimSuffix(baseURL, "/api")
		return baseURL + "/api/tags"
	}
	return baseURL + "/models"
}

// FetchFromProvider makes a GET request to a provider's endpoint
func FetchFromProvider(ctx context.Context, p *config.Provider, endpoint string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}

// WriteOllamaModelsResponse processes Ollama's model response
func WriteOllamaModelsResponse(w http.ResponseWriter, body io.ReadCloser) {
	defer func() { _ = body.Close() }()

	var ollamaResp struct {
		Models []struct {
			Name       string `json:"name"`
			ModifiedAt string `json:"modified_at"`
			Digest     string `json:"digest"`
			Details    struct {
				Format            string   `json:"format"`
				Family            string   `json:"family"`
				ParameterSize     string   `json:"parameter_size"`
				QuantizationLevel string   `json:"quantization_level"`
				Families          []string `json:"families"`
			} `json:"details"`
			Size int64 `json:"size"`
		} `json:"models"`
	}

	if err := json.NewDecoder(body).Decode(&ollamaResp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := make([]map[string]interface{}, 0, len(ollamaResp.Models))
	for _, model := range ollamaResp.Models {
		data = append(data, map[string]interface{}{
			"id":         model.Name,
			"object":     "model",
			"created":    0,
			"owned_by":   "ollama",
			"permission": []interface{}{},
			"root":       model.Name,
			"parent":     nil,
		})
	}

	WriteJSON(w, map[string]interface{}{
		"object": "list",
		"data":   data,
	})
}

// FindOllamaModelFunc is a mockable version of FindOllamaModel
var FindOllamaModelFunc = FindOllamaModel

// FindOpenAIModelFunc is a mockable version of FindOpenAIModel
var FindOpenAIModelFunc = FindOpenAIModel

// FindOllamaModel finds a model in Ollama response
func FindOllamaModel(w http.ResponseWriter, body io.ReadCloser, modelName string) {
	defer func() { _ = body.Close() }()

	var ollamaResp struct {
		Models []struct {
			Name       string `json:"name"`
			ModifiedAt string `json:"modified_at"`
			Digest     string `json:"digest"`
			Details    struct {
				Format            string   `json:"format"`
				Family            string   `json:"family"`
				ParameterSize     string   `json:"parameter_size"`
				QuantizationLevel string   `json:"quantization_level"`
				Families          []string `json:"families"`
			} `json:"details"`
			Size int64 `json:"size"`
		} `json:"models"`
	}

	if err := json.NewDecoder(body).Decode(&ollamaResp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, m := range ollamaResp.Models {
		if m.Name == modelName {
			WriteJSON(w, map[string]interface{}{
				"id":       m.Name,
				"object":   "model",
				"created":  0,
				"owned_by": "ollama",
				"size":     m.Size,
				"digest":   m.Digest,
				"details": map[string]interface{}{
					"format":             m.Details.Format,
					"family":             m.Details.Family,
					"families":           m.Details.Families,
					"parameter_size":     m.Details.ParameterSize,
					"quantization_level": m.Details.QuantizationLevel,
				},
			})
			return
		}
	}

	http.Error(w, "Model not found", http.StatusNotFound)
}

// FindOpenAIModel finds a model in OpenAI response
func FindOpenAIModel(w http.ResponseWriter, body io.ReadCloser, modelName string) {
	defer func() { _ = body.Close() }()

	var result map[string]interface{}
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data, ok := result["data"].([]interface{})
	if !ok {
		WriteJSON(w, result)
		return
	}

	for _, m := range data {
		model, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		if id, _ := model["id"].(string); id == modelName {
			WriteJSON(w, model)
			return
		}
	}

	http.Error(w, "Model not found", http.StatusNotFound)
}

var (
	readProxyRequestFunc = readProxyRequest
	proxyProxyFunc       = proxy.ProxyFunc
	proxyStreamFunc      = proxy.Stream
)

func readProxyRequest(r *http.Request) (*proxy.Request, error) {
	// Limit request body size to prevent memory exhaustion
	r.Body = http.MaxBytesReader(nil, r.Body, maxRequestBodySize)

	// Get request from pool
	req := proxy.RequestPool.Get().(*proxy.Request)
	// Ensure we start with a clean state (but keep allocated slices capacity)
	req.Model = ""
	req.Messages = req.Messages[:0]
	req.Tools = nil
	req.ToolChoice = nil
	req.MaxTokens = 0
	req.Temperature = 0
	req.TopP = 0
	req.Stream = false
	req.ExtraParams = nil
	req.Probe = false
	req.RawBody = req.RawBody[:0]
	req.Provider = ""

	// Create tee reader to preserve raw body while decoding
	var bodyBuffer bytes.Buffer
	teeReader := io.TeeReader(r.Body, &bodyBuffer)

	// Decode JSON directly from stream
	if err := json.NewDecoder(teeReader).Decode(req); err != nil {
		// Put back the request if we fail to decode
		proxy.RequestPool.Put(req)
		return nil, fmt.Errorf("failed to decode request: %w", err)
	}

	req.RawBody = bodyBuffer.Bytes()
	return req, nil
}

// WriteJSON writes data as JSON to ResponseWriter
func WriteJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Error().Err(err).Msg("Failed to encode JSON response")
	}
}

func errorString(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

// TrimTrailingSlash removes trailing slashes
func TrimTrailingSlash(url string) string {
	for len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}
	return url
}

// TrimSuffix removes a suffix
func TrimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}
