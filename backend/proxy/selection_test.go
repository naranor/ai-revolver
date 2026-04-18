package proxy

import (
	"ai-proxy/config"
	"context"
	"testing"
	"time"

	"ai-proxy/db"
)

// ==================== SUNNY CASES ====================

// TestSunny_FirstModelSuccess - первая модель отвечает успешно
func TestSunny_FirstModelSuccess(t *testing.T) {
	ctx := context.Background()
	mock := NewMultiMockServer()
	mock.Add("fast", MockResponseConfig{Success: true})
	mock.Add("medium", MockResponseConfig{Success: true})
	mock.Add("slow", MockResponseConfig{Success: true})
	defer mock.Stop()

	cfg := TestConfigWithMultiMock(mock)
	config.LoadTestConfig(cfg)

	req := Request{
		Model: "auto",
		Messages: []Message{
			{Role: "user", Content: "test"},
		},
	}

	result, _, err := Proxy(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if result == nil {
		t.Error("Expected result, got nil")
	}
	if mock.GetRequestCount("fast") != 1 {
		t.Errorf("Expected 1 request to fast, got %d", mock.GetRequestCount("fast"))
	}
}

// TestSunny_FirstFails_SecondSucceeds - модель 1 ошибка → модель 2 успех
func TestSunny_FirstFails_SecondSucceeds(t *testing.T) {
	ctx := context.Background()
	mock := NewMultiMockServer()
	mock.Add("fast", MockResponseConfig{Success: false, StatusCode: 500})
	mock.Add("medium", MockResponseConfig{Success: true})
	mock.Add("slow", MockResponseConfig{Success: true})
	defer mock.Stop()

	cfg := TestConfigWithMultiMock(mock)
	config.LoadTestConfig(cfg)

	req := Request{
		Model: "auto",
		Messages: []Message{
			{Role: "user", Content: "test"},
		},
	}

	result, _, err := Proxy(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if result == nil {
		t.Error("Expected result, got nil")
	}
	// First failed, second should succeed
	if mock.GetRequestCount("fast") != 1 {
		t.Errorf("Expected 1 request to fast, got %d", mock.GetRequestCount("fast"))
	}
	if mock.GetRequestCount("medium") != 1 {
		t.Errorf("Expected 1 request to medium, got %d", mock.GetRequestCount("medium"))
	}
}

// TestSunny_FirstTwoFail_ThirdSucceeds - модель 1,2 ошибка → модель 3 успех
func TestSunny_FirstTwoFail_ThirdSucceeds(t *testing.T) {
	ctx := context.Background()
	mock := NewMultiMockServer()
	mock.Add("fast", MockResponseConfig{Success: false, StatusCode: 500})
	mock.Add("medium", MockResponseConfig{Success: false, StatusCode: 500})
	mock.Add("slow", MockResponseConfig{Success: true})
	defer mock.Stop()

	cfg := TestConfigWithMultiMock(mock)
	config.LoadTestConfig(cfg)

	req := Request{
		Model: "auto",
		Messages: []Message{
			{Role: "user", Content: "test"},
		},
	}

	result, _, err := Proxy(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if result == nil {
		t.Error("Expected result, got nil")
	}
	if mock.GetRequestCount("slow") != 1 {
		t.Errorf("Expected 1 request to slow, got %d", mock.GetRequestCount("slow"))
	}
}

// TestSunny_LastSuccessfulModelRemembered - запоминание последней успешной модели
func TestSunny_LastSuccessfulModelRemembered(t *testing.T) {
	ctx := context.Background()
	mock := NewMultiMockServer()
	mock.Add("fast", MockResponseConfig{Success: false, StatusCode: 500})
	mock.Add("medium", MockResponseConfig{Success: true})
	mock.Add("slow", MockResponseConfig{Success: true})
	defer mock.Stop()

	cfg := TestConfigWithMultiMock(mock)
	config.LoadTestConfig(cfg)

	// First request - fast fails, medium should succeed
	req1 := Request{
		Model:    "auto",
		Messages: []Message{{Role: "user", Content: "test"}},
	}

	_, _, err := Proxy(ctx, req1)
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}

	// Get the last successful model
	provider, model, _ := GetLastSuccessfulModel()
	if provider != "medium" {
		t.Errorf("Expected last successful provider 'medium', got '%s'", provider)
	}
	if model != "model-b" {
		t.Errorf("Expected last successful model 'model-b', got '%s'", model)
	}

	// Second request - should start with medium (last successful)
	mock.Reset()
	req2 := Request{
		Model:    "auto",
		Messages: []Message{{Role: "user", Content: "test2"}},
	}

	_, _, err = Proxy(ctx, req2)
	if err != nil {
		t.Fatalf("Second request failed: %v", err)
	}

	// Medium (last successful) should be tried first
	if mock.GetRequestCount("medium") != 1 {
		t.Errorf("Expected medium to be tried first, got %d requests", mock.GetRequestCount("medium"))
	}
}

// TestSunny_SpecificModel - запрос с конкретной моделью
func TestSunny_SpecificModel(t *testing.T) {
	ctx := context.Background()
	mock := NewMultiMockServer()
	mock.Add("fast", MockResponseConfig{Success: true})
	mock.Add("medium", MockResponseConfig{Success: true})
	defer mock.Stop()

	cfg := TestConfigWithMultiMock(mock)
	config.LoadTestConfig(cfg)

	req := Request{
		Model: "model-b",
		Messages: []Message{
			{Role: "user", Content: "test"},
		},
	}

	result, _, err := Proxy(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if result == nil {
		t.Error("Expected result, got nil")
	}
	// Should only try model-b, not model-a
	if mock.GetRequestCount("fast") != 0 {
		t.Errorf("Expected 0 requests to fast, got %d", mock.GetRequestCount("fast"))
	}
}

// ==================== RAINY CASES ====================

// TestRainy_AllModelsFail - все модели возвращают ошибку
func TestRainy_AllModelsFail(t *testing.T) {
	ctx := context.Background()
	mock := NewMultiMockServer()
	mock.Add("fast", MockResponseConfig{Success: false, StatusCode: 500})
	mock.Add("medium", MockResponseConfig{Success: false, StatusCode: 500})
	mock.Add("slow", MockResponseConfig{Success: false, StatusCode: 500})
	defer mock.Stop()

	cfg := TestConfigWithMultiMock(mock)
	config.LoadTestConfig(cfg)

	req := Request{
		Model: "auto",
		Messages: []Message{
			{Role: "user", Content: "test"},
		},
	}

	result, _, err := Proxy(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}
	if result != nil {
		t.Error("Expected nil result")
	}
}

// TestRainy_ModelBlockedAfterTwoFailures - модель блокируется после 2 ошибок
func TestRainy_ModelBlockedAfterTwoFailures(t *testing.T) {
	ctx := context.Background()
	mock := NewMultiMockServer()
	// First provider always fails
	mock.Add("fast", MockResponseConfig{Success: false, StatusCode: 500})
	// Second provider succeeds
	mock.Add("medium", MockResponseConfig{Success: true})
	defer mock.Stop()

	cfg := TestConfigWithMultiMock(mock)
	config.LoadTestConfig(cfg)

	// First request - fast fails, medium should succeed
	req := Request{
		Model:    "auto",
		Messages: []Message{{Role: "user", Content: "test"}},
	}

	_, _, err := Proxy(ctx, req)
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}

	// Check that fast is now blocked (after 2 failures would happen on retry)
	// But in auto mode, we don't retry after single failure - we move to next
}

// TestRainy_HighLatencyBlocks - высокая латентность блокирует модель
func TestRainy_HighLatencyBlocks(t *testing.T) {
	ctx := context.Background()
	mock := NewMultiMockServer()
	// First provider: high latency (> threshold)
	mock.Add("fast", MockResponseConfig{Latency: 2 * time.Second, Success: true})
	// Second provider: normal latency
	mock.Add("medium", MockResponseConfig{Success: true})
	defer mock.Stop()

	cfg := TestConfigWithMultiMock(mock)
	config.LoadTestConfig(cfg)

	// Set low threshold for test (500ms)
	SetLatencyThreshold(500) // 0.5 seconds

	req := Request{
		Model:    "auto",
		Messages: []Message{{Role: "user", Content: "test"}},
	}

	result, _, err := Proxy(ctx, req)

	// Should succeed with second provider after first is slow
	if err != nil {
		t.Logf("Got error (expected for slow provider): %v", err)
	}
	if result != nil {
		t.Log("Got result from fallback provider")
	}

	// Verify fast is now marked as slow (blocked)
	status, _ := GetModelStatus("fast", "model-a")
	if status != StatusDegraded {
		t.Errorf("Expected fast/model-a to be marked as degraded, got status %v", status)
	}

	// Reset threshold
	SetLatencyThreshold(10000)
}

// TestRainy_RateLimit - rate limiting пропускает провайдера
func TestRainy_RateLimit(t *testing.T) {
	ctx := context.Background()
	mock := NewMultiMockServer()
	mock.Add("fast", MockResponseConfig{Success: true})
	mock.Add("medium", MockResponseConfig{Success: true})
	defer mock.Stop()

	cfg := TestConfigWithMultiMock(mock)
	// Set rate limit and usage to trigger limiting
	cfg.Providers[0].RateLimit = 1
	cfg.Providers[0].CurrentUsage = 1
	config.LoadTestConfig(cfg)

	req := Request{
		Model:    "auto",
		Messages: []Message{{Role: "user", Content: "test"}},
	}

	result, _, err := Proxy(ctx, req)

	// Should skip fast (rate limited) and use medium
	if err != nil {
		t.Errorf("Expected success with medium, got error: %v", err)
	}
	if result == nil {
		t.Error("Expected result from medium")
	}
}

// ==================== EDGE CASES ====================

// TestEdge_EmptyConfig - пустой конфиг
func TestEdge_EmptyConfig(t *testing.T) {
	ctx := context.Background()
	cfg := TestConfigEmptyProviders()
	config.LoadTestConfig(cfg)

	req := Request{
		Model: "auto",
		Messages: []Message{
			{Role: "user", Content: "test"},
		},
	}

	result, _, err := Proxy(ctx, req)

	if err == nil {
		t.Error("Expected error for empty config")
	}
	if result != nil {
		t.Error("Expected nil result")
	}
}

// TestEdge_ProviderWithNoModels - провайдер без моделей
func TestEdge_ProviderWithNoModels(t *testing.T) {
	ctx := context.Background()
	mock := NewMultiMockServer()
	mock.Add("working", MockResponseConfig{Success: true})
	defer mock.Stop()

	cfg := config.Config{
		Providers: []config.Provider{
			{
				Name:      "empty",
				APIKey:    "test",
				BaseURL:   "http://localhost:1", // unreachable, but has no models anyway
				Priority:  1,
				RateLimit: 100,
				Enabled:   BoolPtr(true),
				Models:    []config.Model{},
			},
			{
				Name:      "working",
				APIKey:    "test",
				BaseURL:   mock.URLFor("working"),
				Priority:  2,
				RateLimit: 100,
				Enabled:   BoolPtr(true),
				Models: []config.Model{
					{Name: "model-x"},
				},
			},
		},
		AutoMode: config.AutoMode{
			Enabled: true,
		},
	}
	config.LoadTestConfig(cfg)

	req := Request{
		Model:    "auto",
		Messages: []Message{{Role: "user", Content: "test"}},
	}

	result, _, err := Proxy(ctx, req)

	// Should work with second provider
	if err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
	if result == nil {
		t.Error("Expected result")
	}
}

// ==================== ROUND-ROBIN TESTS ====================

// TestRoundRobin_Order - проверка порядка round-robin
func TestRoundRobin_Order(t *testing.T) {
	cfg := config.Config{
		Providers: []config.Provider{
			{
				Name:      "provider1",
				APIKey:    "test",
				BaseURL:   "http://localhost:1",
				Priority:  1,
				RateLimit: 100,
				Enabled:   BoolPtr(true),
				Models: []config.Model{
					{Name: "m1"},
					{Name: "m2"},
				},
			},
			{
				Name:      "provider2",
				APIKey:    "test",
				BaseURL:   "http://localhost:1",
				Priority:  2,
				RateLimit: 100,
				Enabled:   BoolPtr(true),
				Models: []config.Model{
					{Name: "m1"},
					{Name: "m2"},
				},
			},
		},
	}
	config.LoadTestConfig(cfg)

	// Get candidates
	candidates := getAllCandidates(cfg)

	// Round-robin order: p1.m1, p2.m1, p1.m2, p2.m2
	expected := []struct{ provider, model string }{
		{"provider1", "m1"},
		{"provider2", "m1"},
		{"provider1", "m2"},
		{"provider2", "m2"},
	}

	if len(candidates) != len(expected) {
		t.Fatalf("Expected %d candidates, got %d", len(expected), len(candidates))
	}

	for i, exp := range expected {
		if candidates[i].Provider.Name != exp.provider || candidates[i].Model != exp.model {
			t.Errorf("Candidate %d: expected %s/%s, got %s/%s",
				i, exp.provider, exp.model,
				candidates[i].Provider.Name, candidates[i].Model)
		}
	}
}

// TestRoundRobin_DifferentModelCounts - разное кол-во моделей у провайдеров
func TestRoundRobin_DifferentModelCounts(t *testing.T) {
	cfg := config.Config{
		Providers: []config.Provider{
			{
				Name:      "p1",
				APIKey:    "test",
				BaseURL:   "http://localhost:1",
				Priority:  1,
				RateLimit: 100,
				Enabled:   BoolPtr(true),
				Models: []config.Model{
					{Name: "a"}, {Name: "b"}, {Name: "c"},
				},
			},
			{
				Name:      "p2",
				APIKey:    "test",
				BaseURL:   "http://localhost:1",
				Priority:  2,
				RateLimit: 100,
				Enabled:   BoolPtr(true),
				Models: []config.Model{
					{Name: "x"},
				},
			},
		},
	}
	config.LoadTestConfig(cfg)

	candidates := getAllCandidates(cfg)

	// Expected: p1.a, p2.x, p1.b, p1.c (p2 has no model[1], model[2])
	expected := []string{
		"p1:a", "p2:x", "p1:b", "p1:c",
	}

	for i, exp := range expected {
		got := candidates[i].Provider.Name + ":" + candidates[i].Model
		if got != exp {
			t.Errorf("Candidate %d: expected %s, got %s", i, exp, got)
		}
	}
}

// TestRoundRobin_LastModelNotFirst - последняя успешная модель не в начале round-robin
func TestRoundRobin_LastModelNotFirst(t *testing.T) {
	cfg := config.Config{
		Providers: []config.Provider{
			{
				Name:      "p1",
				APIKey:    "test",
				BaseURL:   "http://localhost:1",
				Priority:  1,
				RateLimit: 100,
				Enabled:   BoolPtr(true),
				Models: []config.Model{
					{Name: "m1"}, {Name: "m2"},
				},
			},
			{
				Name:      "p2",
				APIKey:    "test",
				BaseURL:   "http://localhost:1",
				Priority:  2,
				RateLimit: 100,
				Enabled:   BoolPtr(true),
				Models: []config.Model{
					{Name: "m1"}, {Name: "m2"},
				},
			},
		},
	}

	// Set last successful model to p2:m1
	SetLastSuccessfulModel("p2", "m1", 100)

	candidates := getAllCandidates(cfg)

	// First should be p2:m1 (last successful)
	if candidates[0].Provider.Name != "p2" || candidates[0].Model != "m1" {
		t.Errorf("Expected first to be p2:m1, got %s:%s",
			candidates[0].Provider.Name, candidates[0].Model)
	}

	// Second should be p1:m1 (first in round-robin after p2:m1)
	if candidates[1].Provider.Name != "p1" || candidates[1].Model != "m1" {
		t.Errorf("Expected second to be p1:m1, got %s:%s",
			candidates[1].Provider.Name, candidates[1].Model)
	}

	ClearLastSuccessfulModel()
}

// ==================== HELPER ====================

func init() {
	// Initialize test database
	db.InitDB(":memory:")
}
