package proxy

import (
	"ai-proxy/config"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRetryLimit(t *testing.T) {
	ResetTestState()
	multiMock := NewMultiMockServer().
		Add("fast", MockResponseConfig{Success: false, StatusCode: 500}).
		Add("medium", MockResponseConfig{Success: false, StatusCode: 500}).
		Add("slow", MockResponseConfig{Success: false, StatusCode: 500})
	defer multiMock.Stop()

	cfg := TestConfigWithMultiMock(multiMock)
	cfg.MaxRetries = 2
	config.LoadTestConfig(cfg)

	req := Request{Model: "auto"}
	candidates := getCandidates(cfg, "auto", "")
	_, code, _ := tryProviders(context.Background(), cfg, candidates, req)

	if code != 500 {
		t.Errorf("Expected status 500, got %d", code)
	}

	// Should only try 2 times
	count1 := multiMock.GetRequestCount("fast")
	count2 := multiMock.GetRequestCount("medium")
	count3 := multiMock.GetRequestCount("slow")

	if count1+count2+count3 != 2 {
		t.Errorf("Expected 2 attempts total, got %d (fast:%d, medium:%d, slow:%d)",
			count1+count2+count3, count1, count2, count3)
	}
}

func Test400ErrorFailover(t *testing.T) {
	ResetTestState()
	multiMock := NewMultiMockServer().
		Add("fast", MockResponseConfig{Success: false, StatusCode: 400}).
		Add("medium", MockResponseConfig{Success: true, StatusCode: 200})
	defer multiMock.Stop()

	cfg := TestConfigWithMultiMock(multiMock)
	cfg.MaxRetries = 5
	config.LoadTestConfig(cfg)

	req := Request{Model: "auto"}
	candidates := getCandidates(cfg, "auto", "")
	_, code, err := tryProviders(context.Background(), cfg, candidates, req)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if code != 200 {
		t.Errorf("Expected status 200, got %d", code)
	}

	if multiMock.GetRequestCount("fast") != 1 {
		t.Errorf("Expected 1 attempt on fast, got %d", multiMock.GetRequestCount("fast"))
	}
	if multiMock.GetRequestCount("medium") != 1 {
		t.Errorf("Expected 1 attempt on medium, got %d", multiMock.GetRequestCount("medium"))
	}
}

func TestStreamingPreWriteFailover(t *testing.T) {
	ResetTestState()
	multiMock := NewMultiMockServer().
		Add("fast", MockResponseConfig{Success: false, StatusCode: 500}).
		Add("medium", MockResponseConfig{Success: true, StatusCode: 200})
	defer multiMock.Stop()

	cfg := TestConfigWithMultiMock(multiMock)
	cfg.MaxRetries = 5
	config.LoadTestConfig(cfg)

	req := Request{Model: "auto", Stream: true}
	rr := httptest.NewRecorder()

	err := Stream(context.Background(), req, rr)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if rr.Code != 200 {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	if multiMock.GetRequestCount("fast") != 1 {
		t.Errorf("Expected 1 attempt on fast, got %d", multiMock.GetRequestCount("fast"))
	}
	if multiMock.GetRequestCount("medium") != 1 {
		t.Errorf("Expected 1 attempt on medium, got %d", multiMock.GetRequestCount("medium"))
	}
}

func TestWarmupSynergy(t *testing.T) {
	ResetTestState()
	multiMock := NewMultiMockServer().
		Add("fast", MockResponseConfig{Success: false, StatusCode: 500}).
		Add("medium", MockResponseConfig{Success: true, StatusCode: 200})
	defer multiMock.Stop()

	cfg := TestConfigWithMultiMock(multiMock)
	cfg.MaxRetries = 5
	config.LoadTestConfig(cfg)

	// Drain StatusEvents channel
drain:
	for {
		select {
		case <-StatusEvents:
		default:
			break drain
		}
	}

	req := Request{Model: "auto"}
	candidates := getCandidates(cfg, "auto", "")
	_, _, _ = tryProviders(context.Background(), cfg, candidates, req)

	// Verify StatusEvent was emitted for "fast" provider failure
	select {
	case event := <-StatusEvents:
		if event.Provider != "fast" {
			t.Errorf("Expected event for provider fast, got %s", event.Provider)
		}
		if event.NewStatus != StatusBlockedTemp {
			t.Errorf("Expected status BlockedTemp, got %d", event.NewStatus)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for StatusEvent")
	}
}

func TestStreamingAllProvidersSkippedReturnsError(t *testing.T) {
	ResetTestState()

	cfg := config.Config{
		Providers: []config.Provider{
			{
				Name:         "fast",
				BaseURL:      "http://example.invalid",
				Priority:     1,
				RateLimit:    1,
				CurrentUsage: 1,
				Enabled:      BoolPtr(true),
				Models:       []config.Model{{Name: "model-a"}},
			},
			{
				Name:         "slow",
				BaseURL:      "http://example.invalid",
				Priority:     2,
				RateLimit:    1,
				CurrentUsage: 1,
				Enabled:      BoolPtr(true),
				Models:       []config.Model{{Name: "model-b"}},
			},
		},
		MaxRetries: 5,
		AutoMode:   config.AutoMode{Enabled: true},
	}
	config.LoadTestConfig(cfg)

	req := Request{Model: "auto", Stream: true}
	rr := httptest.NewRecorder()

	err := Stream(context.Background(), req, rr)
	if err == nil {
		t.Fatal("Expected stream error when all providers are skipped")
	}
}

func TestStreamingPostWriteFailureRecordsFailure(t *testing.T) {
	ResetTestState()

	streamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		flusher.Flush()

		// Add enough bytes above scanner buffer limit to trigger bufio.ErrTooLong deterministically.
		largePayload := strings.Repeat("a", int(OpenAIStreamBufferSize)+1024)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", largePayload)
		flusher.Flush()
	}))
	defer streamSrv.Close()

	cfg := config.Config{
		Providers: []config.Provider{
			{
				Name:     "streaming",
				BaseURL:  streamSrv.URL,
				Priority: 1,
				Enabled:  BoolPtr(true),
				Models:   []config.Model{{Name: "model-a"}},
			},
		},
		MaxRetries: 1,
		AutoMode:   config.AutoMode{Enabled: true},
	}
	config.LoadTestConfig(cfg)

	req := Request{Model: "auto", Stream: true}
	rr := httptest.NewRecorder()

	err := Stream(context.Background(), req, rr)
	if err == nil {
		t.Fatal("Expected stream error after oversized SSE chunk")
	}

	status, _ := GetModelStatus("streaming", "model-a")
	if status != StatusBlockedTemp {
		t.Fatalf("Expected model status %v after stream failure, got %v", StatusBlockedTemp, status)
	}
}
