package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-proxy/config"
	"ai-proxy/proxy"
)

func TestHangingProviderFailover(t *testing.T) {
	// 1. Setup mock server with a slow provider and a fast one
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second): // Delay longer than 2s timeout
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"slow"}}]}`))
		case <-r.Context().Done():
			return
		}
	}))
	defer slowServer.Close()

	fastServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"fast"}}]}`))
	}))
	defer fastServer.Close()

	// 2. Configure proxy with these providers and short timeout
	proxy.ResetTestState()
	cfg := config.Config{
		ResponseTimeoutSeconds: 2, // Short timeout for testing
		Providers: []config.Provider{
			{
				Name:      "slow-provider",
				BaseURL:   slowServer.URL,
				Priority:  1,
				RateLimit: 100,
				Enabled:   proxy.BoolPtr(true),
				Models:    []config.Model{{Name: "m1"}},
			},
			{
				Name:      "fast-provider",
				BaseURL:   fastServer.URL,
				Priority:  2,
				RateLimit: 100,
				Enabled:   proxy.BoolPtr(true),
				Models:    []config.Model{{Name: "m1"}},
			},
		},
	}
	config.LoadTestConfig(cfg)
	// Re-init HTTP clients to pick up new timeouts
	proxy.InitHTTPClients(100, 10, 30*time.Second, 1*time.Second, 2*time.Second)
	
	ctx := context.Background()
	req := proxy.Request{
		Model:    "m1",
		Messages: []proxy.Message{{Role: "user", Content: "test"}},
	}

	startTime := time.Now()
	result, _, err := proxy.Proxy(ctx, req)
	duration := time.Since(startTime)

	// 4. Verify
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}
	if result == nil || len(result.Choices) == 0 {
		t.Fatal("Expected result choices")
	}
	content := result.Choices[0].Message.GetContentString()
	if content != "fast" {
		t.Errorf("Expected response from fast provider, got: %s", content)
	}
	// It should take ~2s because slow-provider is tried first and it hangs
	if duration < 2*time.Second {
		t.Errorf("Expected failover after ~2s, took %v", duration)
	}
}

func TestClientDisconnect(t *testing.T) {
	// 1. Setup mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			return
		}
	}))
	defer mockServer.Close()

	proxy.ResetTestState()
	cfg := config.Config{
		Providers: []config.Provider{
			{
				Name:      "provider",
				BaseURL:   mockServer.URL,
				Priority:  1,
				RateLimit: 100,
				Enabled:   proxy.BoolPtr(true),
				Models:    []config.Model{{Name: "m1"}},
			},
		},
	}
	config.LoadTestConfig(cfg)

	// 2. Start request with cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	req := proxy.Request{
		Model:    "m1",
		Messages: []proxy.Message{{Role: "user", Content: "test"}},
	}

	errChan := make(chan error, 1)
	go func() {
		_, _, err := proxy.Proxy(ctx, req)
		errChan <- err
	}()

	// 3. Cancel after a short delay
	time.Sleep(200 * time.Millisecond)
	cancel()

	// 4. Verify Proxy returned cancellation error
	err := <-errChan
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("Expected context canceled error, got: %v", err)
	}
}

func TestLargePayload(t *testing.T) {
	// Verify that ReadTimeout: 0 works by sending large payload
	// Since we are testing internal proxy.Proxy calls here, it's more about forwardRequest.
	
	largeContent := strings.Repeat("a", 2*1024*1024) // 2MB
	
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) < 2*1024*1024 {
			t.Errorf("Expected large body, got %d bytes", len(body))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer mockServer.Close()

	proxy.ResetTestState()
	cfg := config.Config{
		Providers: []config.Provider{
			{
				Name:      "provider",
				BaseURL:   mockServer.URL,
				Priority:  1,
				RateLimit: 100,
				Enabled:   proxy.BoolPtr(true),
				Models:    []config.Model{{Name: "m1"}},
			},
		},
	}
	config.LoadTestConfig(cfg)

	ctx := context.Background()
	req := proxy.Request{
		Model:    "m1",
		Messages: []proxy.Message{{Role: "user", Content: largeContent}},
	}

	result, _, err := proxy.Proxy(ctx, req)
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}
	if result.Choices[0].Message.GetContentString() != "ok" {
		t.Errorf("Expected 'ok', got: %s", result.Choices[0].Message.GetContentString())
	}
}
