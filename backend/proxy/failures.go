// Package proxy implements the core logic for intelligent model selection and request forwarding
package proxy

import (
	"ai-proxy/logger"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ModelStatus represents the current health state of a model
type ModelStatus int

// ModelStatusEvent represents a change in a model's operational status
type ModelStatusEvent struct {
	Provider  string
	Model     string
	OldStatus ModelStatus
	NewStatus ModelStatus
}

// StatusEvents is a buffered channel for model status change events
var StatusEvents = make(chan ModelStatusEvent, 100)

// emitStatusChange sends a status change event to the StatusEvents channel (non-blocking)
func emitStatusChange(provider, model string, old, newStatus ModelStatus) {
	if old == newStatus {
		return
	}
	event := ModelStatusEvent{
		Provider:  provider,
		Model:     model,
		OldStatus: old,
		NewStatus: newStatus,
	}
	select {
	case StatusEvents <- event:
	default:
		// Channel full, drop event to avoid blocking
	}
}

const (
	// StatusActive means the model is fully operational
	StatusActive ModelStatus = iota
	// StatusDegraded means the model is slow but still operational
	StatusDegraded
	// StatusBlockedTemp means the model is temporarily disabled due to errors
	StatusBlockedTemp
	// StatusBlockedFatal means the model is permanently disabled
	StatusBlockedFatal
)

// FailureReason represents why a model was moved out of StatusActive
type FailureReason int

const (
	// ReasonNone indicates no failure
	ReasonNone FailureReason = iota
	// ReasonSlow indicates the model exceeded latency threshold
	ReasonSlow
	// ReasonRateLimit indicates the provider returned 429
	ReasonRateLimit
	// ReasonServerError indicates the provider returned 5xx
	ReasonServerError
	// ReasonTimeout indicates a network or context timeout
	ReasonTimeout
	// ReasonFatal indicates a 401 or 404 error
	ReasonFatal
)

// modelTrack tracks the state and performance of a specific model on a specific provider
type modelTrack struct {
	lastFailure  time.Time
	blockedUntil time.Time
	status       ModelStatus
	reason       FailureReason
	failureCount int
	ewmaLatency  float64
	backoffLevel int
	lastHTTPCode int
}

// failuresTracker manages model failure tracking
type failuresTracker struct {
	failures         map[string]*modelTrack
	providerFailures map[string]int
	mu               sync.RWMutex
}

// Global failures tracker
var tracker = &failuresTracker{
	failures:         make(map[string]*modelTrack),
	providerFailures: make(map[string]int),
}

// OnPersistentFailure is called when a provider fails 3 times consecutively
var OnPersistentFailure func(provider string, lastError error)

// failureCooldown is the duration to block a model after 2 failures (default 5 minutes)
var failureCooldown = 5 * time.Minute

// failureThreshold is the number of failures before blocking
const failureThreshold = 1

// latencyThreshold is the latency in ms above which a model is considered slow
var latencyThreshold int64 = 10000

// SetBlockDuration sets the duration to block a model after failures
var SetBlockDuration = func(duration time.Duration) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	failureCooldown = duration
}

// GetBlockDuration returns the current block duration
var GetBlockDuration = func() time.Duration {
	tracker.mu.RLock()
	defer tracker.mu.RUnlock()
	return failureCooldown
}

// SetLatencyThreshold sets the latency threshold for marking models as slow
var SetLatencyThreshold = func(threshold int64) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	latencyThreshold = threshold
}

// GetLatencyThreshold returns the current latency threshold
var GetLatencyThreshold = func() int64 {
	tracker.mu.RLock()
	defer tracker.mu.RUnlock()
	return latencyThreshold
}

// makeKey creates a unique key for provider:model combination
func makeKey(provider, model string) string {
	return provider + ":" + model
}

// RecordFailure records a failure for a model and blocks it if threshold reached
func RecordFailure(provider, model string, httpCode int, err error) {
	key := makeKey(provider, model)

	tracker.mu.Lock()

	track, exists := tracker.failures[key]
	if !exists {
		track = &modelTrack{status: StatusActive}
		tracker.failures[key] = track
	}

	oldStatus := track.status
	track.lastFailure = time.Now()
	track.lastHTTPCode = httpCode

	// Track consecutive failures per provider (for legacy callback)
	tracker.providerFailures[provider]++
	providerFailureCount := tracker.providerFailures[provider]

	// Trigger callback if provider fails 3 times consecutively
	callback := OnPersistentFailure
	if providerFailureCount >= 3 && callback != nil {
		// Unlock to avoid deadlock in callback
		tracker.mu.Unlock()
		callback(provider, err)
		tracker.mu.Lock()
	}

	// Classification Logic
	switch {
	case httpCode == http.StatusUnauthorized || httpCode == http.StatusNotFound:
		// Fatal error: block forever
		track.status = StatusBlockedFatal
		track.reason = ReasonFatal
		track.blockedUntil = time.Now().Add(365 * 24 * time.Hour) // Practical "forever"
		logger.Error().
			Str("provider", provider).
			Str("model", model).
			Int("http_code", httpCode).
			Msg("Model blocked FATAL (401/404)")

	case httpCode == http.StatusTooManyRequests:
		// Rate Limit: block for 5 minutes (standard default)
		track.status = StatusBlockedTemp
		track.reason = ReasonRateLimit
		track.blockedUntil = time.Now().Add(5 * time.Minute)
		track.failureCount = 0 // Reset error count as it's a limit, not a bug
		logger.Warn().
			Str("provider", provider).
			Str("model", model).
			Msg("Model blocked TEMP (429 Rate Limit) for 5m")

	case httpCode == http.StatusBadRequest:
		// Bad Request: block for 5 minutes
		track.status = StatusBlockedTemp
		track.reason = ReasonServerError
		track.blockedUntil = time.Now().Add(5 * time.Minute)
		track.failureCount = 0 // Reset error count as it's a client error/misconfig
		logger.Warn().
			Str("provider", provider).
			Str("model", model).
			Int("http_code", httpCode).
			Msg("Model blocked TEMP (400 Bad Request) for 5m")

	case httpCode >= 500 || httpCode == 0: // 0 often means timeout/network error
		track.failureCount++
		track.reason = ReasonServerError
		if httpCode == 0 {
			track.reason = ReasonTimeout
		}

		// Block if threshold reached (2 errors)
		if track.failureCount >= failureThreshold {
			track.status = StatusBlockedTemp
			track.backoffLevel++
			// Exponential backoff: (2^level) * 2 minutes
			backoffDuration := time.Duration(1<<track.backoffLevel) * 2 * time.Minute
			track.blockedUntil = time.Now().Add(backoffDuration)
			logger.Warn().
				Str("provider", provider).
				Str("model", model).
				Int("http_code", httpCode).
				Int("failure_count", track.failureCount).
				Dur("blocked_for", backoffDuration).
				Msg("Model blocked TEMP (5xx/Timeout) with backoff")
		} else {
			logger.Debug().
				Str("provider", provider).
				Str("model", model).
				Int("failure_count", track.failureCount).
				Msg("Model recorded failure (pending block)")
		}

	default:
		// Other errors: block for 5 minutes
		track.status = StatusBlockedTemp
		track.reason = ReasonServerError
		track.blockedUntil = time.Now().Add(5 * time.Minute)
		logger.Warn().
			Str("provider", provider).
			Str("model", model).
			Int("http_code", httpCode).
			Msg("Model blocked TEMP (Other error) for 5m")
	}

	newStatus := track.status
	tracker.mu.Unlock()
	emitStatusChange(provider, model, oldStatus, newStatus)
}

// RecordSuccess records a successful request, updates EWMA latency and resets failures
func RecordSuccess(provider, model string, latency int64) {
	key := makeKey(provider, model)

	tracker.mu.Lock()

	// Reset consecutive failures for the provider on success
	tracker.providerFailures[provider] = 0

	track, exists := tracker.failures[key]
	if !exists {
		track = &modelTrack{status: StatusActive}
		tracker.failures[key] = track
	}

	oldStatus := track.status

	// Update EWMA: New_Average = (0.8 * Old_Average) + (0.2 * New_Latency)
	if track.ewmaLatency == 0 {
		track.ewmaLatency = float64(latency)
	} else {
		track.ewmaLatency = (0.8 * track.ewmaLatency) + (0.2 * float64(latency))
	}

	// Reset failure-related fields
	track.failureCount = 0
	track.backoffLevel = 0

	// Update status based on EWMA threshold
	wasBlocked := !track.blockedUntil.IsZero() && time.Now().Before(track.blockedUntil)
	track.blockedUntil = time.Time{} // Clear any temporary block

	if latencyThreshold > 0 && track.ewmaLatency > float64(latencyThreshold) {
		track.status = StatusDegraded
		track.reason = ReasonSlow
		logger.Warn().
			Str("provider", provider).
			Str("model", model).
			Float64("ewma_latency_ms", track.ewmaLatency).
			Int64("threshold_ms", latencyThreshold).
			Msg("Model degraded due to high EWMA latency")
	} else {
		track.status = StatusActive
		track.reason = ReasonNone
		if wasBlocked {
			logger.Info().
				Str("provider", provider).
				Str("model", model).
				Msg("Model recovered, moved to StatusActive")
		}
	}

	newStatus := track.status
	tracker.mu.Unlock()
	emitStatusChange(provider, model, oldStatus, newStatus)
}

// GetModelStatus returns the current status and reason for a model
func GetModelStatus(provider, model string) (ModelStatus, FailureReason) {
	key := makeKey(provider, model)

	tracker.mu.RLock()
	defer tracker.mu.RUnlock()

	track, exists := tracker.failures[key]
	if !exists {
		return StatusActive, ReasonNone
	}

	if track.status == StatusBlockedFatal {
		return StatusBlockedFatal, track.reason
	}

	// Check if temporary block has expired
	if track.status == StatusBlockedTemp {
		if time.Now().After(track.blockedUntil) {
			// If it's still slow via EWMA, it stays degraded, otherwise active
			if latencyThreshold > 0 && track.ewmaLatency > float64(latencyThreshold) {
				return StatusDegraded, ReasonSlow
			}
			return StatusActive, ReasonNone
		}
		// Still blocked
		return StatusBlockedTemp, track.reason
	}

	return track.status, track.reason
}

// GetModelLatency returns the last recorded latency for a model
func GetModelLatency(provider, model string) int64 {
	key := makeKey(provider, model)

	tracker.mu.RLock()
	defer tracker.mu.RUnlock()

	track, exists := tracker.failures[key]
	if !exists {
		return 0
	}

	return int64(track.ewmaLatency)
}

// ResetFailures resets the failure count for a model (called on success)
func ResetFailures(provider, model string) {
	key := makeKey(provider, model)

	tracker.mu.Lock()

	// Reset consecutive failures for the provider on success
	tracker.providerFailures[provider] = 0

	if track, exists := tracker.failures[key]; exists {
		oldStatus := track.status
		wasBlocked := !track.blockedUntil.IsZero() && time.Now().Before(track.blockedUntil)
		track.failureCount = 0
		track.blockedUntil = time.Time{} // zero time = not blocked
		track.status = StatusActive
		track.reason = ReasonNone
		track.backoffLevel = 0

		if wasBlocked {
			logger.Info().
				Str("provider", provider).
				Str("model", model).
				Msg("Model recovered, removed from inactive list")
		}
		tracker.mu.Unlock()
		emitStatusChange(provider, model, oldStatus, StatusActive)
	} else {
		tracker.mu.Unlock()
	}
}

// CleanupOldEntries removes old failure entries (older than cooldown period)
func CleanupOldEntries() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	now := time.Now()
	for key, track := range tracker.failures {
		// Remove entry if:
		// 1. Not currently blocked AND last failure was long ago
		// 2. Never had failures (lastFailure is zero) - cleanup after cooldown
		if now.After(track.blockedUntil) {
			if track.lastFailure.IsZero() || now.Sub(track.lastFailure) > failureCooldown {
				delete(tracker.failures, key)
			}
		}
	}
}

// GetFailureCount returns the current failure count for a model
func GetFailureCount(provider, model string) int {
	key := makeKey(provider, model)

	tracker.mu.RLock()
	defer tracker.mu.RUnlock()

	if track, exists := tracker.failures[key]; exists {
		return track.failureCount
	}
	return 0
}

// GetBlockedModels returns a map of all currently blocked models
// Key format: "provider:model"
var GetBlockedModels = func() map[string]map[string]interface{} {
	tracker.mu.RLock()
	defer tracker.mu.RUnlock()

	blocked := make(map[string]map[string]interface{})
	now := time.Now()

	for key, track := range tracker.failures {
		if now.Before(track.blockedUntil) || track.status == StatusBlockedFatal {
			blocked[key] = map[string]interface{}{
				"blocked":   true,
				"latency":   int64(track.ewmaLatency),
				"is_slow":   track.status == StatusDegraded,
				"reason":    int(track.reason),
				"status":    int(track.status),
				"last_code": track.lastHTTPCode,
			}
		}
	}

	return blocked
}

// GetBestFallbackModel returns the provider:model with lowest latency from blocked models
// Returns empty string if no blocked models with latency data
func GetBestFallbackModel() (string, string, int64) {
	tracker.mu.RLock()
	defer tracker.mu.RUnlock()

	now := time.Now()
	var bestProvider, bestModel string
	var bestLatency float64 = -1

	for key, track := range tracker.failures {
		// Only consider models that are not fatal
		if track.status == StatusBlockedFatal {
			continue
		}

		// Consider currently blocked or degraded models
		if now.Before(track.blockedUntil) || track.status == StatusDegraded {
			if bestLatency == -1 || (track.ewmaLatency > 0 && track.ewmaLatency < bestLatency) {
				// Parse key: "provider:model"
				if provider, model, found := strings.Cut(key, ":"); found {
					bestProvider = provider
					bestModel = model
					bestLatency = track.ewmaLatency
				}
			}
		}
	}

	if bestLatency == -1 {
		return "", "", 0
	}

	return bestProvider, bestModel, int64(bestLatency)
}

// ResetAllFailures clears all failure tracking (for testing)
func ResetAllFailures() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.failures = make(map[string]*modelTrack)
	tracker.providerFailures = make(map[string]int)
}
