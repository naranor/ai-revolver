package proxy

import (
	"sync"
	"time"
)

// ProviderMetrics holds observability data for a single provider
type ProviderMetrics struct {
	lastRequests   []time.Time
	latencies      []int64
	successHistory []bool
	RPM            float64 `json:"rpm"`
	Latency        float64 `json:"latency_ms"`
	ErrorRate      float64 `json:"error_rate"`
	TotalRequests  int64   `json:"total_requests"`
	TotalErrors    int64   `json:"total_errors"`
}

// MetricsStore manages metrics for all providers
type MetricsStore struct {
	metrics map[string]*ProviderMetrics
	window  time.Duration
	mu      sync.RWMutex
}

// GlobalMetricsStore is the singleton instance
var GlobalMetricsStore = &MetricsStore{
	metrics: make(map[string]*ProviderMetrics),
	window:  time.Minute,
}

// ReportRequest updates metrics for a provider
func (ms *MetricsStore) ReportRequest(provider string, latencyMs int64, err error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	pm, ok := ms.metrics[provider]
	if !ok {
		pm = &ProviderMetrics{
			lastRequests:   make([]time.Time, 0, 100),
			latencies:      make([]int64, 0, 10),
			successHistory: make([]bool, 0, 100),
		}
		ms.metrics[provider] = pm
	}

	now := time.Now()
	pm.TotalRequests++
	pm.lastRequests = append(pm.lastRequests, now)

	// Track success/failure
	isSuccess := err == nil
	if !isSuccess {
		pm.TotalErrors++
	}
	pm.successHistory = append(pm.successHistory, isSuccess)

	// Track latency (moving average of last 10)
	if isSuccess {
		pm.latencies = append(pm.latencies, latencyMs)
		if len(pm.latencies) > 10 {
			pm.latencies = pm.latencies[1:]
		}

		var sum int64
		for _, l := range pm.latencies {
			sum += l
		}
		pm.Latency = float64(sum) / float64(len(pm.latencies))
	}

	// Cleanup old data and calculate RPM/ErrorRate
	ms.cleanup(pm, now)
}

func (ms *MetricsStore) cleanup(pm *ProviderMetrics, now time.Time) {
	// RPM Cleanup (sliding window)
	cutoff := now.Add(-ms.window)
	i := 0
	for i < len(pm.lastRequests) && pm.lastRequests[i].Before(cutoff) {
		i++
	}
	pm.lastRequests = pm.lastRequests[i:]
	pm.RPM = float64(len(pm.lastRequests))

	// Error Rate Cleanup (last 100 requests)
	if len(pm.successHistory) > 100 {
		pm.successHistory = pm.successHistory[len(pm.successHistory)-100:]
	}

	if len(pm.successHistory) > 0 {
		var errors int
		for _, success := range pm.successHistory {
			if !success {
				errors++
			}
		}
		pm.ErrorRate = float64(errors) / float64(len(pm.successHistory))
	}
}

// GetMetrics returns a copy of metrics for all providers
func (ms *MetricsStore) GetMetrics() map[string]ProviderMetrics {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	now := time.Now()
	result := make(map[string]ProviderMetrics)
	for name, pm := range ms.metrics {
		ms.cleanup(pm, now) // Ensure up-to-date RPM
		result[name] = *pm
	}
	return result
}

// GetStats is an alias for GetMetrics for MCP resources
func (ms *MetricsStore) GetStats() map[string]ProviderMetrics {
	return ms.GetMetrics()
}

// Reset clears all metrics (primarily for testing)
func (ms *MetricsStore) Reset() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.metrics = make(map[string]*ProviderMetrics)
}
