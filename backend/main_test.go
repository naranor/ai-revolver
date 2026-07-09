package main

import (
	"ai-proxy/config"
	"ai-proxy/db"
	"ai-proxy/proxy"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStatsHandler(t *testing.T) {
	// Mock the db.GetStatsWithPeriod function
	originalGetStatsWithPeriod := db.GetStatsWithPeriod
	db.GetStatsWithPeriod = func(_ string) (map[string]interface{}, error) {
		return map[string]interface{}{
			"total_requests": 100,
			"models": []map[string]interface{}{
				{
					"provider": "test-provider",
					"model":    "test-model",
				},
			},
		}, nil
	}
	defer func() {
		db.GetStatsWithPeriod = originalGetStatsWithPeriod
	}()

	// Mock the proxy.GetBlockedModels function
	originalGetBlockedModels := proxy.GetBlockedModels
	proxy.GetBlockedModels = func() map[string]map[string]interface{} {
		return map[string]map[string]interface{}{
			"test-provider:test-model": {
				"is_slow":   true,
				"latency":   1234,
				"last_code": 500,
			},
		}
	}
	defer func() {
		proxy.GetBlockedModels = originalGetBlockedModels
	}()

	// Mock the proxy.GetLatencyThreshold function
	originalGetLatencyThreshold := proxy.GetLatencyThreshold
	proxy.GetLatencyThreshold = func() int64 {
		return 5000
	}
	defer func() {
		proxy.GetLatencyThreshold = originalGetLatencyThreshold
	}()

	// Mock the proxy.GetBlockDuration function
	originalGetBlockDuration := proxy.GetBlockDuration
	proxy.GetBlockDuration = func() time.Duration {
		return 5 * time.Minute
	}
	defer func() {
		proxy.GetBlockDuration = originalGetBlockDuration
	}()

	req, err := http.NewRequestWithContext(context.Background(), 
"GET", "/api/v1/stats", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(statsHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to unmarshal response body: %v", err)
	}

	if stats["total_requests"] != 100.0 {
		t.Errorf("Expected total_requests to be 100, got %v", stats["total_requests"])
	}

	models, ok := stats["models"].([]interface{})
	if !ok {
		t.Fatal("Expected models to be a slice")
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}
	model, ok := models[0].(map[string]interface{})
	if !ok {
		t.Fatal("Expected model to be a map")
	}
	if model["is_blocked"] != true {
		t.Error("Expected model to be blocked")
	}
}

func TestStatsHandler_Error(t *testing.T) {
	// Mock the db.GetStatsWithPeriod function to return an error
	originalGetStatsWithPeriod := db.GetStatsWithPeriod
	db.GetStatsWithPeriod = func(_ string) (map[string]interface{}, error) {
		return nil, errors.New("test error")
	}
	defer func() {
		db.GetStatsWithPeriod = originalGetStatsWithPeriod
	}()

	req, err := http.NewRequestWithContext(context.Background(), 
"GET", "/api/v1/stats", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(statsHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestStatsHistoryHandler(t *testing.T) {
	// Mock the db.GetStatsHistoryWithPeriod function
	originalGetStatsHistoryWithPeriod := db.GetStatsHistoryWithPeriod
	db.GetStatsHistoryWithPeriod = func(_ int, _ string) ([]map[string]interface{}, error) {
		return []map[string]interface{}{
			{
				"timestamp":      "2024-01-01T00:00:00Z",
				"total_requests": 10,
			},
		}, nil
	}
	defer func() {
		db.GetStatsHistoryWithPeriod = originalGetStatsHistoryWithPeriod
	}()

	req, err := http.NewRequestWithContext(context.Background(), 
"GET", "/api/v1/stats/history", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(statsHistoryHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	var history []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &history); err != nil {
		t.Fatalf("Failed to unmarshal response body: %v", err)
	}

	if len(history) != 1 {
		t.Fatalf("Expected 1 history point, got %d", len(history))
	}
	if history[0]["total_requests"] != 10.0 {
		t.Errorf("Expected total_requests to be 10, got %v", history[0]["total_requests"])
	}
}

func TestStatsHistoryHandler_Error(t *testing.T) {
	// Mock the db.GetStatsHistoryWithPeriod function to return an error
	originalGetStatsHistoryWithPeriod := db.GetStatsHistoryWithPeriod
	db.GetStatsHistoryWithPeriod = func(_ int, _ string) ([]map[string]interface{}, error) {
		return nil, errors.New("test error")
	}
	defer func() {
		db.GetStatsHistoryWithPeriod = originalGetStatsHistoryWithPeriod
	}()

	req, err := http.NewRequestWithContext(context.Background(), 
"GET", "/api/v1/stats/history", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(statsHistoryHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestModelsHandler(t *testing.T) {
	// Mock the config.GetConfig function
	originalGetConfig := config.GetConfig
	config.GetConfig = func() config.Config {
		return config.Config{
			Providers: []config.Provider{
				{
					Name: "test-provider",
					Models: []config.Model{
						{
							Name: "test-model",
						},
					},
				},
			},
		}
	}
	defer func() {
		config.GetConfig = originalGetConfig
	}()

	req, err := http.NewRequestWithContext(context.Background(), 
"GET", "/api/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(modelsHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	var models OpenAIModelsList
	if err := json.Unmarshal(rr.Body.Bytes(), &models); err != nil {
		t.Fatalf("Failed to unmarshal response body: %v", err)
	}

	if len(models.Data) != 2 {
		t.Fatalf("Expected 2 models, got %d", len(models.Data))
	}
	if models.Data[1].ID != "test-provider/test-model" {
		t.Errorf("Expected model ID to be 'test-provider/test-model', got '%s'", models.Data[1].ID)
	}
}

func TestConfigHandler(t *testing.T) {
	// GET
	originalGetConfig := config.GetConfig
	config.GetConfig = func() config.Config {
		return config.Config{
			Providers: []config.Provider{
				{
					Name: "test-provider",
				},
			},
		}
	}
	defer func() {
		config.GetConfig = originalGetConfig
	}()

	req, err := http.NewRequestWithContext(context.Background(), 
"GET", "/api/v1/config", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(configHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	var cfg config.Config
	if err := json.Unmarshal(rr.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("Failed to unmarshal response body: %v", err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("Expected 1 provider, got %d", len(cfg.Providers))
	}

	// PUT with bad body
	req, err = http.NewRequestWithContext(context.Background(), 
"PUT", "/api/v1/config", bytes.NewReader([]byte("invalid")))
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code for bad request: got %v want %v",
			status, http.StatusInternalServerError)
	}

	// PUT with good body
	originalUpdateConfig := config.UpdateConfig
	config.UpdateConfig = func(_ []byte) error {
		return nil
	}
	defer func() {
		config.UpdateConfig = originalUpdateConfig
	}()

	newCfg := config.Config{
		Providers: []config.Provider{
			{
				Name: "new-provider",
			},
		},
	}
	body, _ := json.Marshal(newCfg)
	req, err = http.NewRequestWithContext(context.Background(), 
"PUT", "/api/v1/config", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code for good request: got %v want %v",
			status, http.StatusOK)
	}

	// PUT with error
	config.UpdateConfig = func(_ []byte) error {
		return errors.New("test error")
	}
	req, err = http.NewRequestWithContext(context.Background(), 
"PUT", "/api/v1/config", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code for error request: got %v want %v",
			status, http.StatusInternalServerError)
	}

	// POST
	req, err = http.NewRequestWithContext(context.Background(), 
"POST", "/api/v1/config", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusMethodNotAllowed {
		t.Errorf("handler returned wrong status code for method not allowed: got %v want %v",
			status, http.StatusMethodNotAllowed)
	}
}

func TestActiveModelHandler(t *testing.T) {
	// Mock the proxy.GetLastSuccessfulModel function
	originalGetLastSuccessfulModel := proxy.GetLastSuccessfulModel
	proxy.GetLastSuccessfulModel = func() (string, string, int64) {
		return "test-provider", "test-model", 123
	}
	defer func() {
		proxy.GetLastSuccessfulModel = originalGetLastSuccessfulModel
	}()

	req, err := http.NewRequestWithContext(context.Background(), 
"GET", "/api/v1/active-model", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(activeModelHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	var activeModel map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &activeModel); err != nil {
		t.Fatalf("Failed to unmarshal response body: %v", err)
	}

	if activeModel["provider"] != "test-provider" {
		t.Errorf("Expected provider to be 'test-provider', got '%v'", activeModel["provider"])
	}

	// POST
	req, err = http.NewRequestWithContext(context.Background(), 
"POST", "/api/v1/active-model", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusMethodNotAllowed {
		t.Errorf("handler returned wrong status code for method not allowed: got %v want %v",
			status, http.StatusMethodNotAllowed)
	}
}

func TestScoutHandler(t *testing.T) {
	// Mock the FindProvider function
	originalFindProvider := FindProviderFunc
	FindProviderFunc = func(name string) *config.Provider {
		if name == "test-provider" {
			return &config.Provider{
				Name: "test-provider",
			}
		}
		if name == "ollama" {
			return &config.Provider{
				Name: "ollama",
			}
		}
		return nil
	}
	defer func() {
		FindProviderFunc = originalFindProvider
	}()

	// Mock the BuildModelsEndpoint function
	originalBuildModelsEndpoint := BuildModelsEndpointFunc
	BuildModelsEndpointFunc = func(_ *config.Provider) string {
		return "http://localhost/models"
	}
	defer func() {
		BuildModelsEndpointFunc = originalBuildModelsEndpoint
	}()

	// Mock the FetchFromProvider function
	originalFetchFromProvider := FetchFromProviderFunc
	FetchFromProviderFunc = func(_ context.Context, _ *config.Provider, _ string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte(`{"data":[]}`))), nil
	}
	defer func() {
		FetchFromProviderFunc = originalFetchFromProvider
	}()

	// Mock the WriteOllamaModelsResponse function
	originalWriteOllamaModelsResponse := WriteOllamaModelsResponseFunc
	WriteOllamaModelsResponseFunc = func(w http.ResponseWriter, _ io.ReadCloser) {
		w.WriteHeader(http.StatusOK)
	}
	defer func() {
		WriteOllamaModelsResponseFunc = originalWriteOllamaModelsResponse
	}()

	// Test provider not found
	req, err := http.NewRequestWithContext(context.Background(), 
"GET", "/api/v1/scout?provider=not-found", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(scoutHandler)
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("handler returned wrong status code for provider not found: got %v want %v",
			status, http.StatusNotFound)
	}

	// Test fetch error
	FetchFromProviderFunc = func(_ context.Context, _ *config.Provider, _ string) (io.ReadCloser, error) {
		return nil, errors.New("test error")
	}
	req, err = http.NewRequestWithContext(context.Background(), "GET", 
 "/api/v1/scout?provider=test-provider", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code for fetch error: got %v want %v",
			status, http.StatusInternalServerError)
	}
	FetchFromProviderFunc = func(_ context.Context, _ *config.Provider, _ string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte(`{"data":[]}`))), nil
	}

	// Test ollama
	req, err = http.NewRequestWithContext(context.Background(), "GET", 
 "/api/v1/scout?provider=ollama", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code for ollama: got %v want %v",
			status, http.StatusOK)
	}

	// Test success
	req, err = http.NewRequestWithContext(context.Background(), "GET", 
 "/api/v1/scout?provider=test-provider", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code for success: got %v want %v",
			status, http.StatusOK)
	}
}

func TestModelInfoHandler(t *testing.T) {
	// Mock the FindProvider function
	originalFindProvider := FindProviderFunc
	FindProviderFunc = func(name string) *config.Provider {
		if name == "test-provider" {
			return &config.Provider{
				Name: "test-provider",
			}
		}
		if name == "ollama" {
			return &config.Provider{
				Name: "ollama",
			}
		}
		return nil
	}
	defer func() {
		FindProviderFunc = originalFindProvider
	}()

	// Mock the BuildModelsEndpoint function
	originalBuildModelsEndpoint := BuildModelsEndpointFunc
	BuildModelsEndpointFunc = func(_ *config.Provider) string {
		return "http://localhost/models"
	}
	defer func() {
		BuildModelsEndpointFunc = originalBuildModelsEndpoint
	}()

	// Mock the FetchFromProvider function
	originalFetchFromProvider := FetchFromProviderFunc
	FetchFromProviderFunc = func(_ context.Context, _ *config.Provider, _ string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte(`{"data":[]}`))), nil
	}
	defer func() {
		FetchFromProviderFunc = originalFetchFromProvider
	}()

	// Mock the FindOllamaModel function
	originalFindOllamaModel := FindOllamaModelFunc
	FindOllamaModelFunc = func(w http.ResponseWriter, _ io.ReadCloser, _ string) {
		w.WriteHeader(http.StatusOK)
	}
	defer func() {
		FindOllamaModelFunc = originalFindOllamaModel
	}()

	// Mock the FindOpenAIModel function
	originalFindOpenAIModel := FindOpenAIModelFunc
	FindOpenAIModelFunc = func(w http.ResponseWriter, _ io.ReadCloser, _ string) {
		w.WriteHeader(http.StatusOK)
	}
	defer func() {
		FindOpenAIModelFunc = originalFindOpenAIModel
	}()

	// Test no provider or model
	req, err := http.NewRequestWithContext(context.Background(), 
"GET", "/api/v1/modelinfo", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(modelInfoHandler)
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("handler returned wrong status code for no provider or model: got %v want %v",
			status, http.StatusBadRequest)
	}

	// Test provider not found
	req, err = http.NewRequestWithContext(context.Background(), "GET", 
 "/api/v1/modelinfo?provider=not-found&model=test-model", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("handler returned wrong status code for provider not found: got %v want %v",
			status, http.StatusNotFound)
	}

	// Test fetch error
	FetchFromProviderFunc = func(_ context.Context, _ *config.Provider, _ string) (io.ReadCloser, error) {
		return nil, errors.New("test error")
	}
	req, err = http.NewRequestWithContext(context.Background(), "GET", 
 "/api/v1/modelinfo?provider=test-provider&model=test-model", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code for fetch error: got %v want %v",
			status, http.StatusInternalServerError)
	}
	FetchFromProviderFunc = func(_ context.Context, _ *config.Provider, _ string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte(`{"data":[]}`))), nil
	}

	// Test ollama
	req, err = http.NewRequestWithContext(context.Background(), "GET", 
 "/api/v1/modelinfo?provider=ollama&model=test-model", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code for ollama: got %v want %v",
			status, http.StatusOK)
	}

	// Test success
	req, err = http.NewRequestWithContext(context.Background(), "GET", 
 "/api/v1/modelinfo?provider=test-provider&model=test-model", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code for success: got %v want %v",
			status, http.StatusOK)
	}
}

func TestChatCompletionsHandler(t *testing.T) {
	// Mock readProxyRequest
	originalReadProxyRequest := readProxyRequestFunc
	readProxyRequestFunc = func(_ *http.Request) (*proxy.Request, error) {
		return &proxy.Request{
			Model:  "test-model",
			Stream: false,
		}, nil
	}
	defer func() {
		readProxyRequestFunc = originalReadProxyRequest
	}()

	// Mock proxyStreamFunc
	originalStream := proxyStreamFunc
	proxyStreamFunc = func(_ context.Context, _ proxy.Request, _ http.ResponseWriter) error {
		return nil
	}
	defer func() {
		proxyStreamFunc = originalStream
	}()

	// Mock proxyProxyFunc
	originalProxy := proxyProxyFunc
	proxyProxyFunc = func(_ context.Context, _ proxy.Request) (*proxy.Response, int, error) {
		return &proxy.Response{}, http.StatusOK, nil
	}
	defer func() {
		proxyProxyFunc = originalProxy
	}()

	// Test method not allowed
	req, err := http.NewRequestWithContext(context.Background(), "GET", "/api/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(chatCompletionsHandler)
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusMethodNotAllowed {
		t.Errorf("handler returned wrong status code for method not allowed: got %v want %v",
			status, http.StatusMethodNotAllowed)
	}

	// Test readProxyRequest error
	readProxyRequestFunc = func(_ *http.Request) (*proxy.Request, error) {
		return nil, errors.New("test error")
	}
	req, err = http.NewRequestWithContext(context.Background(), "POST", "/api/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("handler returned wrong status code for readProxyRequest error: got %v want %v",
			status, http.StatusBadRequest)
	}
	readProxyRequestFunc = func(_ *http.Request) (*proxy.Request, error) {
		return &proxy.Request{
			Model:  "test-model",
			Stream: false,
		}, nil
	}

	// Test stream
	readProxyRequestFunc = func(_ *http.Request) (*proxy.Request, error) {
		return &proxy.Request{
			Model:  "test-model",
			Stream: true,
		}, nil
	}
	req, err = http.NewRequestWithContext(context.Background(), "POST", "/api/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code for stream: got %v want %v",
			status, http.StatusOK)
	}

	// Test stream error
	proxyStreamFunc = func(_ context.Context, _ proxy.Request, _ http.ResponseWriter) error {
		return errors.New("test error")
	}
	req, err = http.NewRequestWithContext(context.Background(), "POST", "/api/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code for stream error: got %v want %v",
			status, http.StatusOK)
	}
	proxyStreamFunc = func(_ context.Context, _ proxy.Request, _ http.ResponseWriter) error {
		return nil
	}

	// Test proxy
	readProxyRequestFunc = func(_ *http.Request) (*proxy.Request, error) {
		return &proxy.Request{
			Model:  "test-model",
			Stream: false,
		}, nil
	}
	req, err = http.NewRequestWithContext(context.Background(), "POST", "/api/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code for proxy: got %v want %v",
			status, http.StatusOK)
	}

	// Test proxy error
	proxyProxyFunc = func(_ context.Context, _ proxy.Request) (*proxy.Response, int, error) {
		return nil, http.StatusInternalServerError, errors.New("test error")
	}
	req, err = http.NewRequestWithContext(context.Background(), "POST", "/api/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code for proxy error: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestHealthHandler(t *testing.T) {
	req, err := http.NewRequestWithContext(context.Background(), "GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(healthHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("healthHandler returned wrong status code: got %v want %v", status, http.StatusOK)
	}
	if rr.Body.String() != "OK" {
		t.Errorf("healthHandler returned wrong body: got %q want %q", rr.Body.String(), "OK")
	}
}

func TestTrimTrailingSlash(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"http://example.com/", "http://example.com"},
		{"http://example.com///", "http://example.com"},
		{"http://example.com", "http://example.com"},
		{"", ""},
		{"/", ""},
	}

	for _, tt := range tests {
		result := TrimTrailingSlash(tt.input)
		if result != tt.expected {
			t.Errorf("TrimTrailingSlash(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestTrimSuffix(t *testing.T) {
	tests := []struct {
		s        string
		suffix   string
		expected string
	}{
		{"http://example.com/api", "/api", "http://example.com"},
		{"http://example.com", "/api", "http://example.com"},
		{"hello world", " world", "hello"},
		{"", "/api", ""},
	}

	for _, tt := range tests {
		result := TrimSuffix(tt.s, tt.suffix)
		if result != tt.expected {
			t.Errorf("TrimSuffix(%q, %q) = %q, want %q", tt.s, tt.suffix, result, tt.expected)
		}
	}
}

func TestWriteJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteJSON(rr, map[string]interface{}{"key": "value", "num": 42})

	if rr.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", rr.Header().Get("Content-Type"))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to unmarshal JSON response: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("Expected key 'value', got %v", result["key"])
	}
}

func TestErrorString(t *testing.T) {
	tests := []struct {
		input    error
		expected string
	}{
		{nil, ""},
		{errors.New("some error"), "some error"},
	}

	for _, tt := range tests {
		result := errorString(tt.input)
		if result != tt.expected {
			t.Errorf("errorString(%v) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestFindProvider(t *testing.T) {
	originalGetConfig := config.GetConfig
	config.GetConfig = func() config.Config {
		return config.Config{
			Providers: []config.Provider{
				{Name: "provider-a"},
				{Name: "provider-b"},
			},
		}
	}
	defer func() { config.GetConfig = originalGetConfig }()

	t.Run("found", func(t *testing.T) {
		p := FindProvider("provider-b")
		if p == nil {
			t.Fatal("Expected provider, got nil")
		}
		if p.Name != "provider-b" {
			t.Errorf("Expected provider 'provider-b', got '%s'", p.Name)
		}
	})

	t.Run("not found", func(t *testing.T) {
		p := FindProvider("unknown")
		if p != nil {
			t.Errorf("Expected nil for unknown provider, got %+v", p)
		}
	})
}

func TestBuildModelsEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		provider config.Provider
		expected string
	}{
		{
			name:     "standard provider",
			provider: config.Provider{Name: "openai", BaseURL: "https://api.openai.com/v1"},
			expected: "https://api.openai.com/v1/models",
		},
		{
			name:     "provider with trailing slash",
			provider: config.Provider{Name: "openai", BaseURL: "https://api.openai.com/v1/"},
			expected: "https://api.openai.com/v1/models",
		},
		{
			name:     "native ollama",
			provider: config.Provider{Name: "ollama", BaseURL: "http://localhost:11434"},
			expected: "http://localhost:11434/api/tags",
		},
		{
			name:     "native ollama with /api",
			provider: config.Provider{Name: "ollama", BaseURL: "http://localhost:11434/api"},
			expected: "http://localhost:11434/api/tags",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildModelsEndpoint(&tt.provider)
			if got != tt.expected {
				t.Errorf("BuildModelsEndpoint() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestWriteOllamaModelsResponse(t *testing.T) {
	t.Run("valid response", func(t *testing.T) {
		ollamaBody := `{"models":[{"name":"llama3","modified_at":"2024-01-01","digest":"abc123","size":4000000000,"details":{"format":"gguf","family":"llama","parameter_size":"8B","quantization_level":"Q4_0","families":["llama"]}}]}`
		body := io.NopCloser(bytes.NewReader([]byte(ollamaBody)))

		rr := httptest.NewRecorder()
		WriteOllamaModelsResponse(rr, body)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", rr.Code)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if result["object"] != "list" {
			t.Errorf("Expected object 'list', got %v", result["object"])
		}
		data, ok := result["data"].([]interface{})
		if !ok || len(data) != 1 {
			t.Fatalf("Expected 1 model in data, got %v", result["data"])
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		body := io.NopCloser(bytes.NewReader([]byte("not json")))
		rr := httptest.NewRecorder()
		WriteOllamaModelsResponse(rr, body)
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected 500 for invalid JSON, got %d", rr.Code)
		}
	})
}

func TestFindOllamaModel(t *testing.T) {
	ollamaBody := `{"models":[{"name":"llama3","modified_at":"2024-01-01","digest":"abc","size":1000,"details":{"format":"gguf","family":"llama","parameter_size":"8B","quantization_level":"Q4_0","families":["llama"]}}]}`

	t.Run("model found", func(t *testing.T) {
		body := io.NopCloser(bytes.NewReader([]byte(ollamaBody)))
		rr := httptest.NewRecorder()
		FindOllamaModel(rr, body, "llama3")
		if rr.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", rr.Code)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if result["id"] != "llama3" {
			t.Errorf("Expected id 'llama3', got %v", result["id"])
		}
	})

	t.Run("model not found", func(t *testing.T) {
		body := io.NopCloser(bytes.NewReader([]byte(ollamaBody)))
		rr := httptest.NewRecorder()
		FindOllamaModel(rr, body, "unknown-model")
		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected 404, got %d", rr.Code)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		body := io.NopCloser(bytes.NewReader([]byte("not json")))
		rr := httptest.NewRecorder()
		FindOllamaModel(rr, body, "llama3")
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected 500 for invalid JSON, got %d", rr.Code)
		}
	})
}

func TestFindOpenAIModel(t *testing.T) {
	openAIBody := `{"object":"list","data":[{"id":"gpt-4","object":"model","created":1677858242,"owned_by":"openai"},{"id":"gpt-3.5-turbo","object":"model","created":1677610602,"owned_by":"openai"}]}`

	t.Run("model found", func(t *testing.T) {
		body := io.NopCloser(bytes.NewReader([]byte(openAIBody)))
		rr := httptest.NewRecorder()
		FindOpenAIModel(rr, body, "gpt-4")
		if rr.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", rr.Code)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if result["id"] != "gpt-4" {
			t.Errorf("Expected id 'gpt-4', got %v", result["id"])
		}
	})

	t.Run("model not found", func(t *testing.T) {
		body := io.NopCloser(bytes.NewReader([]byte(openAIBody)))
		rr := httptest.NewRecorder()
		FindOpenAIModel(rr, body, "unknown-model")
		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected 404, got %d", rr.Code)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		body := io.NopCloser(bytes.NewReader([]byte("not json")))
		rr := httptest.NewRecorder()
		FindOpenAIModel(rr, body, "gpt-4")
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("Expected 500 for invalid JSON, got %d", rr.Code)
		}
	})

	t.Run("response without data array returns full result", func(t *testing.T) {
		body := io.NopCloser(bytes.NewReader([]byte(`{"id":"gpt-4","object":"model"}`)))
		rr := httptest.NewRecorder()
		FindOpenAIModel(rr, body, "gpt-4")
		if rr.Code != http.StatusOK {
			t.Errorf("Expected 200 for non-list response, got %d", rr.Code)
		}
	})
}
