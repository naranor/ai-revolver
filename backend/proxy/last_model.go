package proxy

import (
	"sync"
)

// lastSuccessfulModel stores the last successfully used provider:model combination
type lastSuccessfulModel struct {
	provider string
	model    string
	latency  int64
	mu       sync.RWMutex
}

// Global instance to track last successful model
var lastModel = &lastSuccessfulModel{}

// SetLastSuccessfulModel saves the provider:model that last succeeded
func SetLastSuccessfulModel(provider, model string, latency int64) {
	lastModel.mu.Lock()
	defer lastModel.mu.Unlock()

	lastModel.provider = provider
	lastModel.model = model
	lastModel.latency = latency
}

// GetLastSuccessfulModel returns the last successful provider, model and latency
// Returns empty strings and 0 if no successful model recorded
var GetLastSuccessfulModel = func() (string, string, int64) {
	lastModel.mu.RLock()
	defer lastModel.mu.RUnlock()

	return lastModel.provider, lastModel.model, lastModel.latency
}

// ClearLastSuccessfulModel resets the last successful model
func ClearLastSuccessfulModel() {
	lastModel.mu.Lock()
	defer lastModel.mu.Unlock()

	lastModel.provider = ""
	lastModel.model = ""
	lastModel.latency = 0
}

// ClearLastSuccessfulModelIf clears the last successful model only if it matches the given provider:model
func ClearLastSuccessfulModelIf(provider, model string) {
	lastModel.mu.Lock()
	defer lastModel.mu.Unlock()

	if lastModel.provider == provider && lastModel.model == model {
		lastModel.provider = ""
		lastModel.model = ""
		lastModel.latency = 0
	}
}
