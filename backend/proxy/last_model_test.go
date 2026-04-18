package proxy

import (
	"ai-proxy/config"
	"testing"
)

func TestSetLastSuccessfulModel(t *testing.T) {
	// Clear last model first
	ClearLastSuccessfulModel()

	// Set a model with latency
	SetLastSuccessfulModel("test-provider", "test-model", 150)

	// Verify it was set
	provider, model, latency := GetLastSuccessfulModel()
	if provider != "test-provider" {
		t.Errorf("Expected provider 'test-provider', got '%s'", provider)
	}
	if model != "test-model" {
		t.Errorf("Expected model 'test-model', got '%s'", model)
	}
	if latency != 150 {
		t.Errorf("Expected latency 150, got %d", latency)
	}
}

func TestGetLastSuccessfulModel(t *testing.T) {
	// Clear last model first
	ClearLastSuccessfulModel()

	// Initially should be empty
	provider, model, latency := GetLastSuccessfulModel()
	if provider != "" {
		t.Errorf("Expected empty provider, got '%s'", provider)
	}
	if model != "" {
		t.Errorf("Expected empty model, got '%s'", model)
	}
	if latency != 0 {
		t.Errorf("Expected latency 0, got %d", latency)
	}

	// Set a model
	SetLastSuccessfulModel("another-provider", "another-model", 250)

	// Verify it was updated
	provider, model, latency = GetLastSuccessfulModel()
	if provider != "another-provider" {
		t.Errorf("Expected provider 'another-provider', got '%s'", provider)
	}
	if model != "another-model" {
		t.Errorf("Expected model 'another-model', got '%s'", model)
	}
	if latency != 250 {
		t.Errorf("Expected latency 250, got %d", latency)
	}
}

func TestClearLastSuccessfulModel(t *testing.T) {
	// Set a model
	SetLastSuccessfulModel("test-provider", "test-model", 100)

	// Clear it
	ClearLastSuccessfulModel()

	// Verify it was cleared
	provider, model, latency := GetLastSuccessfulModel()
	if provider != "" {
		t.Errorf("Expected empty provider after clear, got '%s'", provider)
	}
	if model != "" {
		t.Errorf("Expected empty model after clear, got '%s'", model)
	}
	if latency != 0 {
		t.Errorf("Expected latency 0 after clear, got %d", latency)
	}
}

func TestGetAllCandidatesWithLastModel(t *testing.T) {
	// Clear last model first
	ClearLastSuccessfulModel()

	// Create a mock config with multiple providers and models
	cfg := config.Config{
		Providers: []config.Provider{
			{
				Name:     "provider1",
				Priority: 1,
				Enabled:  BoolPtr(true),
				Models: []config.Model{
					{Name: "model-a"},
					{Name: "model-b"},
				},
			},
			{
				Name:     "provider2",
				Priority: 2,
				Enabled:  BoolPtr(true),
				Models: []config.Model{
					{Name: "model-c"},
					{Name: "model-d"},
				},
			},
		},
	}

	// Get candidates without last model set
	candidates := getAllCandidates(cfg)

	// Should have 4 candidates in round-robin order
	if len(candidates) != 4 {
		t.Fatalf("Expected 4 candidates, got %d", len(candidates))
	}

	// Round-robin order: provider1.model[0], provider2.model[0], provider1.model[1], provider2.model[1]
	// Expected: model-a, model-c, model-b, model-d
	if candidates[0].Provider.Name != "provider1" || candidates[0].Model != "model-a" {
		t.Errorf("Expected first candidate to be provider1:model-a, got %s:%s", candidates[0].Provider.Name, candidates[0].Model)
	}
	if candidates[1].Provider.Name != "provider2" || candidates[1].Model != "model-c" {
		t.Errorf("Expected second candidate to be provider2:model-c, got %s:%s", candidates[1].Provider.Name, candidates[1].Model)
	}
	if candidates[2].Provider.Name != "provider1" || candidates[2].Model != "model-b" {
		t.Errorf("Expected third candidate to be provider1:model-b, got %s:%s", candidates[2].Provider.Name, candidates[2].Model)
	}
	if candidates[3].Provider.Name != "provider2" || candidates[3].Model != "model-d" {
		t.Errorf("Expected fourth candidate to be provider2:model-d, got %s:%s", candidates[3].Provider.Name, candidates[3].Model)
	}

	// Set last successful model to provider2:model-d
	SetLastSuccessfulModel("provider2", "model-d", 200)

	// Get candidates again
	candidates = getAllCandidates(cfg)

	// Should still have 4 candidates
	if len(candidates) != 4 {
		t.Fatalf("Expected 4 candidates, got %d", len(candidates))
	}

	// First should now be provider2:model-d (last successful)
	if candidates[0].Provider.Name != "provider2" || candidates[0].Model != "model-d" {
		t.Errorf("Expected first candidate to be provider2:model-d, got %s:%s", candidates[0].Provider.Name, candidates[0].Model)
	}

	// Second should be provider1:model-a (first in remaining round-robin)
	if candidates[1].Provider.Name != "provider1" || candidates[1].Model != "model-a" {
		t.Errorf("Expected second candidate to be provider1:model-a, got %s:%s", candidates[1].Provider.Name, candidates[1].Model)
	}
}
