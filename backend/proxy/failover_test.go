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

	_, err := Stream(context.Background(), req, rr)

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

	_, err := Stream(context.Background(), req, rr)
	if err == nil {
		t.Fatal("Expected stream error when all providers are skipped")
	}
}

func TestStreamingFallbackSuccess(t *testing.T) {
	ResetTestState()
	multiMock := NewMultiMockServer().
		Add("fast", MockResponseConfig{Success: false, StatusCode: 500}).
		Add("medium", MockResponseConfig{Success: true, StatusCode: 200})
	defer multiMock.Stop()

	cfg := TestConfigWithMultiMock(multiMock)
	// Keep only the two providers we seeded in multiMock.
	cfg.Providers = cfg.Providers[:2]
	// Force both providers to be skipped in the regular candidate loop (rate-limited).
	for i := range cfg.Providers {
		cfg.Providers[i].RateLimit = 1
		cfg.Providers[i].CurrentUsage = 1
	}
	cfg.MaxRetries = 5
	config.LoadTestConfig(cfg)

	// Seed failure-tracker state so GetBestFallbackModel can pick a candidate.
	// Lower EWMA on "medium" so it wins over "fast".
	RecordSuccess("medium", "model-b", 100)
	RecordFailure("medium", "model-b", 500, fmt.Errorf("temporary"))
	RecordSuccess("fast", "model-a", 5000)
	RecordFailure("fast", "model-a", 500, fmt.Errorf("temporary"))

	req := Request{Model: "auto", Stream: true}
	rr := httptest.NewRecorder()

	_, err := Stream(context.Background(), req, rr)
	if err != nil {
		t.Fatalf("Expected successful stream fallback, got error: %v", err)
	}
	if rr.Code != 200 {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
	if multiMock.GetRequestCount("fast") != 0 {
		t.Errorf("Expected fast provider to stay skipped, got %d requests", multiMock.GetRequestCount("fast"))
	}
	if multiMock.GetRequestCount("medium") != 1 {
		t.Errorf("Expected 1 fallback stream attempt on medium, got %d", multiMock.GetRequestCount("medium"))
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

		// Add 1KB above scanner buffer limit to deterministically trigger bufio.ErrTooLong in tests.
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

	_, err := Stream(context.Background(), req, rr)
	if err == nil {
		t.Fatal("Expected stream error after oversized SSE chunk")
	}

	status, _ := GetModelStatus("streaming", "model-a")
	if status != StatusBlockedTemp {
		t.Fatalf("Expected model status %v after stream failure, got %v", StatusBlockedTemp, status)
	}
}

func TestStreamingEmptySSEFailovers(t *testing.T) {
	ResetTestState()

	var emptyHits, goodHits int
	emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		emptyHits++
		w.Header().Set("Content-Type", "text/event-stream")
		// Upstream "success" with no substantive content — only DONE.
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer emptySrv.Close()

	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		goodHits++
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"from-medium\"}}]}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer goodSrv.Close()

	cfg := config.Config{
		Providers: []config.Provider{
			{Name: "fast", BaseURL: emptySrv.URL, Priority: 1, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-a"}}},
			{Name: "medium", BaseURL: goodSrv.URL, Priority: 2, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-b"}}},
		},
		MaxRetries: 5,
		AutoMode:   config.AutoMode{Enabled: true},
	}
	config.LoadTestConfig(cfg)

	rr := httptest.NewRecorder()
	_, err := Stream(context.Background(), Request{Model: "auto", Stream: true}, rr)
	if err != nil {
		t.Fatalf("Expected successful failover stream, got: %v", err)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "from-medium") {
		t.Fatalf("Expected content from medium provider, got body: %q", body)
	}
	if emptyHits != 1 {
		t.Errorf("Expected 1 empty upstream attempt, got %d", emptyHits)
	}
	if goodHits != 1 {
		t.Errorf("Expected 1 good upstream attempt, got %d", goodHits)
	}
}

func TestStreamingEmptyJSONChoicesFailovers(t *testing.T) {
	ResetTestState()

	var emptyHits, goodHits int
	emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		emptyHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer emptySrv.Close()

	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		goodHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"from-medium"}}]}`))
	}))
	defer goodSrv.Close()

	cfg := config.Config{
		Providers: []config.Provider{
			{Name: "fast", BaseURL: emptySrv.URL, Priority: 1, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-a"}}},
			{Name: "medium", BaseURL: goodSrv.URL, Priority: 2, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-b"}}},
		},
		MaxRetries: 5,
		AutoMode:   config.AutoMode{Enabled: true},
	}
	config.LoadTestConfig(cfg)

	rr := httptest.NewRecorder()
	_, err := Stream(context.Background(), Request{Model: "auto", Stream: true}, rr)
	if err != nil {
		t.Fatalf("Expected successful failover stream, got: %v", err)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "from-medium") {
		t.Fatalf("Expected content from medium provider, got body: %q", body)
	}
	if emptyHits != 1 {
		t.Errorf("Expected 1 empty JSON attempt, got %d", emptyHits)
	}
	if goodHits != 1 {
		t.Errorf("Expected 1 good attempt, got %d", goodHits)
	}
}

func TestNonStreamEmptyContentFailovers(t *testing.T) {
	ResetTestState()

	var emptyHits, goodHits int
	emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		emptyHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":""}}]}`))
	}))
	defer emptySrv.Close()

	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		goodHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"from-medium"}}]}`))
	}))
	defer goodSrv.Close()

	cfg := config.Config{
		Providers: []config.Provider{
			{Name: "fast", BaseURL: emptySrv.URL, Priority: 1, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-a"}}},
			{Name: "medium", BaseURL: goodSrv.URL, Priority: 2, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-b"}}},
		},
		MaxRetries: 5,
		AutoMode:   config.AutoMode{Enabled: true},
	}
	config.LoadTestConfig(cfg)

	result, code, err := Proxy(context.Background(), Request{Model: "auto"})
	if err != nil {
		t.Fatalf("Expected successful failover, got: %v", err)
	}
	if code != 200 {
		t.Errorf("Expected status 200, got %d", code)
	}
	if result == nil || len(result.Choices) == 0 || result.Choices[0].Message.GetContentString() != "from-medium" {
		t.Fatalf("Expected content from medium, got %+v", result)
	}
	if emptyHits != 1 {
		t.Errorf("Expected 1 empty attempt, got %d", emptyHits)
	}
	if goodHits != 1 {
		t.Errorf("Expected 1 good attempt, got %d", goodHits)
	}
}

func TestToolCallsOnlyNotEmpty(t *testing.T) {
	ResetTestState()

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"ping","arguments":"{}"}}]}}]}`))
	}))
	defer srv.Close()

	cfg := config.Config{
		Providers: []config.Provider{
			{Name: "tools", BaseURL: srv.URL, Priority: 1, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-a"}}},
			{Name: "backup", BaseURL: "http://127.0.0.1:1", Priority: 2, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-b"}}},
		},
		MaxRetries: 5,
		AutoMode:   config.AutoMode{Enabled: true},
	}
	config.LoadTestConfig(cfg)

	result, code, err := Proxy(context.Background(), Request{Model: "auto"})
	if err != nil {
		t.Fatalf("Expected tool_calls-only response to succeed, got: %v", err)
	}
	if code != 200 {
		t.Errorf("Expected status 200, got %d", code)
	}
	if result == nil || len(result.Choices) == 0 || result.Choices[0].Message.ToolCalls == nil {
		t.Fatalf("Expected tool_calls in result, got %+v", result)
	}
	if hits != 1 {
		t.Errorf("Expected single attempt (no failover), got %d", hits)
	}
}

func TestStreamingRolePreambleBuffered(t *testing.T) {
	ResetTestState()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	cfg := config.Config{
		Providers:  []config.Provider{{Name: "fast", BaseURL: srv.URL, Priority: 1, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-a"}}}},
		MaxRetries: 1,
		AutoMode:   config.AutoMode{Enabled: true},
	}
	config.LoadTestConfig(cfg)

	rr := httptest.NewRecorder()
	done, err := Stream(context.Background(), Request{Model: "auto", Stream: true}, rr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !done {
		t.Fatal("expected upstreamDone=true when upstream sent [DONE]")
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"role":"assistant"`) {
		t.Fatalf("expected buffered role preamble in body, got %q", body)
	}
	if !strings.Contains(body, "hello") {
		t.Fatalf("expected content in body, got %q", body)
	}
	if strings.Count(body, "[DONE]") != 1 {
		t.Fatalf("expected single [DONE], got %q", body)
	}
}

func TestOllamaEmptyStreamFailovers(t *testing.T) {
	ResetTestState()

	var emptyHits, goodHits int
	emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		emptyHits++
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = fmt.Fprint(w, `{"model":"m","message":{"role":"assistant","content":""},"done":true}`+"\n")
	}))
	defer emptySrv.Close()

	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		goodHits++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"from-medium\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer goodSrv.Close()

	cfg := config.Config{
		Providers: []config.Provider{
			// Name "ollama" + no /v1 => native Ollama stream path
			{Name: "ollama", BaseURL: emptySrv.URL, Priority: 1, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-a"}}},
			{Name: "medium", BaseURL: goodSrv.URL, Priority: 2, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-b"}}},
		},
		MaxRetries: 5,
		AutoMode:   config.AutoMode{Enabled: true},
	}
	config.LoadTestConfig(cfg)

	rr := httptest.NewRecorder()
	_, err := Stream(context.Background(), Request{Model: "auto", Stream: true}, rr)
	if err != nil {
		t.Fatalf("expected failover success, got %v", err)
	}
	if !strings.Contains(rr.Body.String(), "from-medium") {
		t.Fatalf("expected medium content, got %q", rr.Body.String())
	}
	if emptyHits != 1 || goodHits != 1 {
		t.Fatalf("expected 1 empty + 1 good hits, got empty=%d good=%d", emptyHits, goodHits)
	}
}

func TestNonStreamReasoningContentNotEmpty(t *testing.T) {
	ResetTestState()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"thinking..."}}]}`))
	}))
	defer srv.Close()

	cfg := config.Config{
		Providers: []config.Provider{
			{Name: "fast", BaseURL: srv.URL, Priority: 1, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-a"}}},
			{Name: "backup", BaseURL: "http://127.0.0.1:1", Priority: 2, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-b"}}},
		},
		MaxRetries: 5,
		AutoMode:   config.AutoMode{Enabled: true},
	}
	config.LoadTestConfig(cfg)

	result, _, err := Proxy(context.Background(), Request{Model: "auto"})
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if hits != 1 {
		t.Fatalf("expected no failover, hits=%d", hits)
	}
	if result.Choices[0].Message.ReasoningContent == "" {
		t.Fatal("expected reasoning_content preserved")
	}
}

func TestNonStreamWhitespaceContentFailovers(t *testing.T) {
	ResetTestState()
	var emptyHits, goodHits int
	emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		emptyHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"   \n"}}]}`))
	}))
	defer emptySrv.Close()
	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		goodHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer goodSrv.Close()

	cfg := config.Config{
		Providers: []config.Provider{
			{Name: "fast", BaseURL: emptySrv.URL, Priority: 1, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-a"}}},
			{Name: "medium", BaseURL: goodSrv.URL, Priority: 2, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-b"}}},
		},
		MaxRetries: 5,
		AutoMode:   config.AutoMode{Enabled: true},
	}
	config.LoadTestConfig(cfg)

	result, _, err := Proxy(context.Background(), Request{Model: "auto"})
	if err != nil {
		t.Fatalf("expected failover: %v", err)
	}
	if result.Choices[0].Message.GetContentString() != "ok" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if emptyHits != 1 || goodHits != 1 {
		t.Fatalf("hits empty=%d good=%d", emptyHits, goodHits)
	}
}

func TestNonStreamSecondChoiceSubstantive(t *testing.T) {
	ResetTestState()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":""}},{"message":{"role":"assistant","content":"second"}}]}`))
	}))
	defer srv.Close()

	cfg := config.Config{
		Providers:  []config.Provider{{Name: "fast", BaseURL: srv.URL, Priority: 1, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-a"}}}},
		MaxRetries: 1,
		AutoMode:   config.AutoMode{Enabled: true},
	}
	config.LoadTestConfig(cfg)

	result, _, err := Proxy(context.Background(), Request{Model: "auto"})
	if err != nil {
		t.Fatalf("expected success using choice[1]: %v", err)
	}
	if isEmptyCompletion(result) {
		t.Fatal("expected non-empty completion via second choice")
	}
}

func TestEmptyCompletionDoesNotBlockModel(t *testing.T) {
	ResetTestState()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	cfg := config.Config{
		Providers:  []config.Provider{{Name: "fast", BaseURL: srv.URL, Priority: 1, Enabled: BoolPtr(true), Models: []config.Model{{Name: "model-a"}}}},
		MaxRetries: 1,
		AutoMode:   config.AutoMode{Enabled: true},
	}
	config.LoadTestConfig(cfg)

	_, _, err := Proxy(context.Background(), Request{Model: "auto"})
	if err == nil {
		t.Fatal("expected error when only empty provider")
	}
	status, _ := GetModelStatus("fast", "model-a")
	if status == StatusBlockedTemp || status == StatusBlockedFatal {
		t.Fatalf("empty completion must not block model, got %v", status)
	}
	if hits != 1 {
		t.Fatalf("expected 1 hit, got %d", hits)
	}
}
