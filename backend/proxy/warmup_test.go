package proxy

import (
	"ai-proxy/config"
	"ai-proxy/db"
	"context"
	"os"
	"testing"
	"time"
)

func TestWarmupManagerTop2(t *testing.T) {
	ResetAllFailures()
	ClearLastSuccessfulModel()

	cfg := config.Config{
		Providers: []config.Provider{
			{
				Name:     "p1",
				Priority: 1,
				Enabled:  BoolPtr(true),
				Models: []config.Model{
					{Name: "m1"},
					{Name: "m2"},
				},
			},
			{
				Name:     "p2",
				Priority: 2,
				Enabled:  BoolPtr(true),
				Models: []config.Model{
					{Name: "m1"},
				},
			},
		},
	}
	config.LoadTestConfig(cfg)

	wm := NewWarmupManager()
	top2 := wm.getTop2(cfg)

	if len(top2) != 2 {
		t.Fatalf("Expected 2 top models, got %d", len(top2))
	}

	// Based on round-robin: p1:m1, p2:m1, p1:m2
	if top2[0].Provider.Name != "p1" || top2[0].Model != "m1" {
		t.Errorf("Expected first model p1:m1, got %s:%s", top2[0].Provider.Name, top2[0].Model)
	}
	if top2[1].Provider.Name != "p2" || top2[1].Model != "m1" {
		t.Errorf("Expected second model p2:m1, got %s:%s", top2[1].Provider.Name, top2[1].Model)
	}
}

func TestWarmupDebounce(t *testing.T) {
	// Drain StatusEvents
drain:
	for {
		select {
		case <-StatusEvents:
		default:
			break drain
		}
	}

	ResetAllFailures()
	ClearLastSuccessfulModel()

	cfg := config.Config{
		WarmupEnabled:  true,
		WarmupInterval: 3600,
		WarmupDebounce: 1, // 1 second for test
		Providers: []config.Provider{
			{
				Name:     "p1",
				Priority: 1,
				Enabled:  BoolPtr(true),
				Models: []config.Model{
					{Name: "m1"},
				},
			},
		},
	}
	config.LoadTestConfig(cfg)

	// Mock Proxy to track calls
	originalProxy := ProxyFunc
	callCount := 0
	ProxyFunc = func(_ context.Context, req Request) (*Response, int, error) {
		if req.IsWarmup {
			callCount++
		}
		return &Response{}, 200, nil
	}
	defer func() { ProxyFunc = originalProxy }()

	wm := NewWarmupManager()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wm.Start(ctx)

	// Initial warmup should happen immediately
	time.Sleep(100 * time.Millisecond)
	if callCount != 1 {
		t.Errorf("Expected 1 initial warmup call, got %d", callCount)
	}

	// Trigger StatusEvent
	StatusEvents <- ModelStatusEvent{
		Provider:  "p2",
		Model:     "m1",
		OldStatus: StatusActive,
		NewStatus: StatusDegraded,
	}

	// Change config to include p2:m1 so it enters top-2
	cfg.Providers = append(cfg.Providers, config.Provider{
		Name:     "p2",
		Priority: 1,
		Enabled:  BoolPtr(true),
		Models: []config.Model{
			{Name: "m1"},
		},
	})
	config.LoadTestConfig(cfg)

	// Wait less than debounce
	time.Sleep(500 * time.Millisecond)
	if callCount != 1 {
		t.Errorf("Warmup should be debounced, expected 1 call, got %d", callCount)
	}

	// Wait for debounce to expire
	time.Sleep(1 * time.Second)
	if callCount < 2 {
		t.Errorf("Warmup should have triggered after debounce, expected >= 2 calls, got %d", callCount)
	}
	
	wm.Stop()
}

func TestWarmupDBRecording(t *testing.T) {
	dbPath := "test_warmup.db"
	_ = os.Remove(dbPath)
	err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer func() {
		_ = db.Close()
		_ = os.Remove(dbPath)
	}()

	ResetAllFailures()
	ClearLastSuccessfulModel()

	// Mock Proxy to succeed and log to DB
	// We need to use the real Proxy but with mocked transport if possible, 
	// or just call LogRequest directly to verify it works, 
	// but the task says "trigger a warmup".
	
	// I'll mock forwardRequestWithLatency to avoid actual network calls
	// but still go through Proxy -> onSuccess -> db.LogRequest
	
	cfg := config.Config{
		Providers: []config.Provider{
			{
				Name:    "p1",
				Enabled: BoolPtr(true),
				Models: []config.Model{
					{Name: "m1"},
				},
				BaseURL: "http://localhost",
			},
		},
	}
	config.LoadTestConfig(cfg)

	wm := NewWarmupManager()
	
	// We need to call sendWarmupRequest which calls Proxy
	// Proxy calls forwardRequestWithLatency
	// We can't easily mock forwardRequestWithLatency because it's not a variable.
	// But Proxy IS a variable in main.go, but not in proxy package.
	
	// Wait, in main.go:
	// var (
	// 	readProxyRequestFunc = readProxyRequest
	// 	proxyProxyFunc       = proxy.Proxy
	// 	proxyStreamFunc      = proxy.Stream
	// )
	
	// But proxy.Proxy itself is what WarmupManager calls.
	
	// Actually, I can mock the HTTP client!
	// But proxy.go uses a global httpClient.
	
	// Let's see if I can just call LogRequest and verify DB.
	// No, the task wants to "trigger a warmup".
	
	// I'll use a small trick: since I'm in the same package, I can't easily mock internal functions 
	// unless they are variables.
	
	// Wait, WarmupManager.sendWarmupRequest calls Proxy(ctx, req).
	// I can just call wm.sendWarmupRequest("p1", "m1") after manually logging it if I have to, 
	// but I want to be honest.
	
	// Since I modified onSuccess to always call db.LogRequest, 
	// and if Proxy fails, onFailure also calls db.LogRequest.
	
	// So even if it fails because of no real network, it should be logged!
	
	wm.sendWarmupRequest("p1", "m1")
	
	// Wait for DB worker to process
	time.Sleep(200 * time.Millisecond)
	
	// Check DB
	var count int
	err = db.DB.QueryRow("SELECT COUNT(*) FROM requests WHERE is_warmup = 1").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query DB: %v", err)
	}
	
	if count == 0 {
		t.Error("Expected at least 1 warmup record in DB")
	}
}
