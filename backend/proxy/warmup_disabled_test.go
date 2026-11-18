package proxy

import (
	"ai-proxy/config"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestWarmupSkipsDisabledProvider ensures Disarmed (enabled:false) providers
// never receive HTTP from warmup / explicit-provider health paths, while
// enabled providers are still warmed.
func TestWarmupSkipsDisabledProvider(t *testing.T) {
	ResetAllFailures()
	ClearLastSuccessfulModel()

	var armedHits int32
	var disarmedHits int32

	armedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&armedHits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer armedSrv.Close()

	disarmedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&disarmedHits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer disarmedSrv.Close()

	cfg := config.Config{
		WarmupEnabled: true,
		MaxRetries:    5,
		Providers: []config.Provider{
			{
				Name:     "disarmed",
				Priority: 1,
				Enabled:  BoolPtr(false),
				BaseURL:  disarmedSrv.URL,
				Models:   []config.Model{{Name: "m1"}},
			},
			{
				Name:     "armed",
				Priority: 2,
				Enabled:  BoolPtr(true),
				BaseURL:  armedSrv.URL,
				Models:   []config.Model{{Name: "m1"}},
			},
		},
	}
	config.LoadTestConfig(cfg)

	wm := NewWarmupManager()
	top2 := wm.getTop2(cfg)
	if len(top2) != 1 {
		t.Fatalf("expected getTop2 to return only enabled provider, got %d", len(top2))
	}
	if top2[0].Provider.Name != "armed" {
		t.Fatalf("expected armed in top2, got %s", top2[0].Provider.Name)
	}

	// Explicit warmup targeting Disarmed must not hit HTTP (debounce / stale name).
	wm.sendWarmupRequest("disarmed", "m1")
	if atomic.LoadInt32(&disarmedHits) != 0 {
		t.Fatalf("sendWarmupRequest hit disabled provider, hits=%d", disarmedHits)
	}

	// Proxy with Provider set (warmup/probe style) must also skip.
	_, _, err := Proxy(context.Background(), Request{
		Model:    "m1",
		Provider: "disarmed",
		IsWarmup: true,
		Messages: []Message{{Role: "user", Content: "hi"}},
		MaxTokens: 1,
	})
	if err == nil {
		t.Fatal("expected error when warming disabled provider via Proxy")
	}
	if atomic.LoadInt32(&disarmedHits) != 0 {
		t.Fatalf("Proxy hit disabled provider, hits=%d", disarmedHits)
	}

	// Enabled provider still receives warmup HTTP.
	wm.sendWarmupRequest("armed", "m1")
	if atomic.LoadInt32(&armedHits) == 0 {
		t.Fatal("expected enabled provider to receive warmup request")
	}
	if atomic.LoadInt32(&disarmedHits) != 0 {
		t.Fatalf("disabled provider was hit while warming enabled, hits=%d", disarmedHits)
	}
}

func TestGetCandidatesSkipsDisabledSpecificProvider(t *testing.T) {
	cfg := config.Config{
		Providers: []config.Provider{
			{
				Name:    "disarmed",
				Enabled: BoolPtr(false),
				Models:  []config.Model{{Name: "m1"}},
			},
			{
				Name:    "armed",
				Enabled: BoolPtr(true),
				Models:  []config.Model{{Name: "m1"}},
			},
		},
	}

	if got := getCandidates(cfg, "m1", "disarmed"); len(got) != 0 {
		t.Fatalf("expected no candidates for disabled provider, got %d", len(got))
	}
	got := getCandidates(cfg, "m1", "armed")
	if len(got) != 1 || got[0].Provider.Name != "armed" {
		t.Fatalf("expected armed candidate, got %+v", got)
	}
}
