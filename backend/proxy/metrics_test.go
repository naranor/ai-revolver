package proxy

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMetricsStore(t *testing.T) {
	ms := &MetricsStore{
		metrics: make(map[string]*ProviderMetrics),
		window:  time.Second, // Small window for testing
	}

	// Report success
	ms.ReportRequest("openai", 100, nil)
	ms.ReportRequest("openai", 200, nil)

	metrics := ms.GetMetrics()["openai"]
	assert.Equal(t, int64(2), metrics.TotalRequests)
	assert.Equal(t, int64(0), metrics.TotalErrors)
	assert.Equal(t, 150.0, metrics.Latency)
	assert.Equal(t, 2.0, metrics.RPM)
	assert.Equal(t, 0.0, metrics.ErrorRate)

	// Report failure
	ms.ReportRequest("openai", 0, errors.New("fail"))
	metrics = ms.GetMetrics()["openai"]
	assert.Equal(t, int64(3), metrics.TotalRequests)
	assert.Equal(t, int64(1), metrics.TotalErrors)
	assert.InDelta(t, 0.33, metrics.ErrorRate, 0.01)

	// Test RPM cleanup
	time.Sleep(1100 * time.Millisecond)
	metrics = ms.GetMetrics()["openai"]
	assert.Equal(t, 0.0, metrics.RPM)
}
