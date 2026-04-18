package proxy

import (
	"testing"
	"time"
)

func TestRecordFailure(t *testing.T) {
	// Reset tracker
	ResetAllFailures()

	// First failure - should block
	RecordFailure("test-provider", "test-model", 500, nil)
	status, _ := GetModelStatus("test-provider", "test-model")
	if status != StatusBlockedTemp {
		t.Error("Model should be blocked after first failure")
	}
}

func TestGetModelStatus(t *testing.T) {
	// Reset tracker
	ResetAllFailures()

	// Unknown model should be active
	status, _ := GetModelStatus("unknown-provider", "unknown-model")
	if status != StatusActive {
		t.Error("Unknown model should be active")
	}

	// Block a model
	RecordFailure("test-provider", "test-model", 500, nil)

	status, _ = GetModelStatus("test-provider", "test-model")
	if status != StatusBlockedTemp {
		t.Error("Model should be blocked after 1 failure")
	}
}

func TestResetFailures(t *testing.T) {
	// Reset tracker
	ResetAllFailures()

	// Block a model
	RecordFailure("test-provider", "test-model", 500, nil)

	status, _ := GetModelStatus("test-provider", "test-model")
	if status != StatusBlockedTemp {
		t.Error("Model should be blocked")
	}

	// Reset failures
	ResetFailures("test-provider", "test-model")

	status, _ = GetModelStatus("test-provider", "test-model")
	if status != StatusActive {
		t.Error("Model should not be blocked after reset")
	}
}

func TestGetFailureCount(t *testing.T) {
	// Reset tracker
	ResetAllFailures()

	if GetFailureCount("test-provider", "test-model") != 0 {
		t.Error("New model should have 0 failures")
	}

	RecordFailure("test-provider", "test-model", 500, nil)
	if GetFailureCount("test-provider", "test-model") != 1 {
		t.Error("Model should have 1 failure")
	}

	RecordFailure("test-provider", "test-model", 500, nil)
	if GetFailureCount("test-provider", "test-model") != 2 {
		t.Error("Model should have 2 failures")
	}
}

func TestCleanupOldEntries(t *testing.T) {
	// Reset tracker
	ResetAllFailures()

	// Add a failure and block it
	RecordFailure("test-provider", "test-model", 500, nil)

	if len(tracker.failures) != 1 {
		t.Error("Should have 1 failure entry")
	}

	// Manually set last failure to old time (simulating old entry)
	tracker.mu.Lock()
	track := tracker.failures["test-provider:test-model"]
	track.lastFailure = time.Now().Add(-2 * failureCooldown)
	track.blockedUntil = time.Now().Add(-failureCooldown) // unblocked
	tracker.mu.Unlock()

	// Cleanup should remove old entries
	CleanupOldEntries()

	if len(tracker.failures) != 0 {
		t.Error("Old entries should be cleaned up")
	}
}

func TestCooldownExpiration(t *testing.T) {
	// Reset tracker
	ResetAllFailures()

	// Block a model
	RecordFailure("test-provider", "test-model", 500, nil)

	status, _ := GetModelStatus("test-provider", "test-model")
	if status != StatusBlockedTemp {
		t.Error("Model should be blocked")
	}

	// Manually set blockedUntil to past (simulating cooldown expiration)
	tracker.mu.Lock()
	track := tracker.failures["test-provider:test-model"]
	track.blockedUntil = time.Now().Add(-1 * time.Second)
	tracker.mu.Unlock()

	status, _ = GetModelStatus("test-provider", "test-model")
	if status == StatusBlockedTemp {
		t.Error("Model should not be blocked after cooldown expiration")
	}
}

func TestGetBlockedModels(t *testing.T) {
	// Reset tracker
	ResetAllFailures()

	// Initially no blocked models
	blocked := GetBlockedModels()
	if len(blocked) != 0 {
		t.Errorf("Expected 0 blocked models, got %d", len(blocked))
	}

	// Block three models (single strike now)
	RecordFailure("provider1", "model-a", 500, nil)
	RecordFailure("provider2", "model-b", 500, nil)
	RecordFailure("provider3", "model-c", 500, nil)

	blocked = GetBlockedModels()
	if len(blocked) != 3 {
		t.Errorf("Expected 3 blocked models, got %d", len(blocked))
	}

	if _, ok := blocked["provider1:model-a"]; !ok {
		t.Error("provider1:model-a should be blocked")
	}
	if _, ok := blocked["provider2:model-b"]; !ok {
		t.Error("provider2:model-b should be blocked")
	}
	if _, ok := blocked["provider3:model-c"]; !ok {
		t.Error("provider3:model-c should be blocked")
	}
}

func TestRecordSuccess(t *testing.T) {
	// Reset tracker
	ResetAllFailures()

	// Set latency threshold
	SetLatencyThreshold(5000)

	// Record normal latency
	RecordSuccess("provider1", "model-fast", 100)
	status, _ := GetModelStatus("provider1", "model-fast")
	if status != StatusActive {
		t.Error("Fast model should be active")
	}
	if GetModelLatency("provider1", "model-fast") != 100 {
		t.Errorf("Expected latency 100, got %d", GetModelLatency("provider1", "model-fast"))
	}

	// Record high latency - should degrade
	RecordSuccess("provider1", "model-slow", 15000)
	status, _ = GetModelStatus("provider1", "model-slow")
	if status != StatusDegraded {
		t.Error("Slow model should be degraded")
	}
	if GetModelLatency("provider1", "model-slow") != 15000 {
		t.Errorf("Expected latency 15000, got %d", GetModelLatency("provider1", "model-slow"))
	}
}

func TestGetBestFallbackModel(t *testing.T) {
	// Reset tracker
	ResetAllFailures()
	SetLatencyThreshold(5000)

	// Block models with different latencies (all exceed threshold)
	RecordSuccess("provider1", "model-slow", 15000)
	RecordSuccess("provider2", "model-medium", 8000)
	RecordSuccess("provider3", "model-fastest", 6000)

	// All should be degraded due to high latency
	status, _ := GetModelStatus("provider1", "model-slow")
	if status != StatusDegraded {
		t.Error("model-slow should be degraded")
	}

	provider, model, latency := GetBestFallbackModel()
	if provider != "provider3" || model != "model-fastest" {
		t.Errorf("Expected provider3:model-fastest, got %s:%s", provider, model)
	}
	if latency != 6000 {
		t.Errorf("Expected latency 6000, got %d", latency)
	}
}

func TestFatalErrors(t *testing.T) {
	ResetAllFailures()

	// 401 Unauthorized
	RecordFailure("p1", "m1", 401, nil)
	status, reason := GetModelStatus("p1", "m1")
	if status != StatusBlockedFatal || reason != ReasonFatal {
		t.Errorf("Expected fatal block for 401, got status %v reason %v", status, reason)
	}

	// 404 Not Found
	RecordFailure("p2", "m2", 404, nil)
	status, reason = GetModelStatus("p2", "m2")
	if status != StatusBlockedFatal || reason != ReasonFatal {
		t.Errorf("Expected fatal block for 404, got status %v reason %v", status, reason)
	}
}

func TestRateLimitErrors(t *testing.T) {
	ResetAllFailures()

	// 429 Rate Limit
	RecordFailure("p1", "m1", 429, nil)
	status, reason := GetModelStatus("p1", "m1")
	if status != StatusBlockedTemp || reason != ReasonRateLimit {
		t.Errorf("Expected temp block for 429, got status %v reason %v", status, reason)
	}
}

func TestBadRequestBlocking(t *testing.T) {
	ResetAllFailures()

	// 400 Bad Request - should block immediately for 5m
	RecordFailure("p1", "m1", 400, nil)
	status, reason := GetModelStatus("p1", "m1")
	if status != StatusBlockedTemp {
		t.Errorf("Expected temp block for 400, got status %v", status)
	}
	if reason != ReasonServerError {
		t.Errorf("Expected ReasonServerError for 400, got %v", reason)
	}

	tracker.mu.RLock()
	track := tracker.failures["p1:m1"]
	if track.failureCount != 0 {
		t.Errorf("Expected failureCount 0 for 400, got %d", track.failureCount)
	}
	tracker.mu.RUnlock()
}

func TestExponentialBackoff(t *testing.T) {
	ResetAllFailures()

	// First 500 - block with level 1 (4m)
	RecordFailure("p1", "m1", 500, nil)
	status, _ := GetModelStatus("p1", "m1")
	if status != StatusBlockedTemp {
		t.Error("Should block on first 500")
	}

	tracker.mu.RLock()
	track := tracker.failures["p1:m1"]
	if track.backoffLevel != 1 {
		t.Errorf("Expected backoff level 1, got %d", track.backoffLevel)
	}
	tracker.mu.RUnlock()

	// Second 500 - block with level 2 (8m)
	RecordFailure("p1", "m1", 500, nil)
	tracker.mu.RLock()
	track = tracker.failures["p1:m1"]
	if track.backoffLevel != 2 {
		t.Errorf("Expected backoff level 2, got %d", track.backoffLevel)
	}
	tracker.mu.RUnlock()
}

func TestStatusEvents(t *testing.T) {
	// Reset tracker
	ResetAllFailures()

	// Drain any existing events
drain:
	for {
		select {
		case <-StatusEvents:
		default:
			break drain
		}
	}

	// Trigger a failure that leads to a status change (StatusActive -> StatusBlockedTemp)
	// We need 1 failure for StatusBlockedTemp (based on failureThreshold = 1)
	RecordFailure("test-provider", "test-model", 500, nil)

	select {
	case event := <-StatusEvents:
		if event.Provider != "test-provider" || event.Model != "test-model" {
			t.Errorf("Unexpected event data: %+v", event)
		}
		if event.OldStatus != StatusActive || event.NewStatus != StatusBlockedTemp {
			t.Errorf("Unexpected status change: %v -> %v", event.OldStatus, event.NewStatus)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for StatusEvents")
	}

	// Test recovery
	ResetFailures("test-provider", "test-model")
	select {
	case event := <-StatusEvents:
		if event.OldStatus != StatusBlockedTemp || event.NewStatus != StatusActive {
			t.Errorf("Unexpected status change on recovery: %v -> %v", event.OldStatus, event.NewStatus)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for recovery StatusEvent")
	}
}
