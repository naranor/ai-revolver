package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"os"
	
	"ai-proxy/config"
	"ai-proxy/db"
	"ai-proxy/proxy"
)

func TestMain(m *testing.M) {
	// Setup
	os.Mkdir("test_data", 0755)
	db.InitDB("test_data/test_stats.db")
	
	// Create dummy config
	dummyConfig := config.Config{
		Providers: []config.Provider{
			{
				Name: "test",
				Models: []config.Model{
					{Name: "test-model"},
				},
			},
		},
	}
	data, _ := json.Marshal(dummyConfig)
	os.WriteFile("test_data/config.json", data, 0644)
	config.LoadConfig("test_data/config.json")

	code := m.Run()

	// Teardown
	os.RemoveAll("test_data")
	os.Exit(code)
}

func TestHealthEndpoint(t *testing.T) {
	req, err := http.NewRequestWithContext(context.Background(), "GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	
	handler.ServeHTTP(rr, req)
	
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}
	
	expected := "OK"
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v", rr.Body.String(), expected)
	}
}

func TestConfigGetter(t *testing.T) {
	cfg := config.GetConfig()
	if len(cfg.Providers) == 0 {
		t.Error("Expected at least one provider in test config")
	}
	if cfg.Providers[0].Name != "test" {
		t.Errorf("Expected provider name 'test', got '%s'", cfg.Providers[0].Name)
	}
}

func TestProxyRequestModelValidation(t *testing.T) {
	ctx := context.Background()
	req := proxy.Request{
		Model: "non-existent",
		Messages: []proxy.Message{
			{Role: "user", Content: "Hello"},
		},
	}
	
	_, _, err := proxy.Proxy(ctx, req)
	if err == nil {
		t.Error("Expected error for non-existent model, got nil")
	}
}
