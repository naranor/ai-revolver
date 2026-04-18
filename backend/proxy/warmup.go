package proxy

import (
	"ai-proxy/config"
	"ai-proxy/logger"
	"context"
	"sync"
	"time"
)

// WarmupManager background service to keep top models warm
type WarmupManager struct {
	inFlight map[string]bool
	stop     chan struct{}
	mu       sync.Mutex
}

// NewWarmupManager creates a new WarmupManager instance
func NewWarmupManager() *WarmupManager {
	return &WarmupManager{
		inFlight: make(map[string]bool),
		stop:     make(chan struct{}),
	}
}

// Start runs the warmup background loop
func (wm *WarmupManager) Start(ctx context.Context) {
	cfg := config.GetConfig()
	if !cfg.WarmupEnabled {
		logger.Info().Msg("Warmup service disabled")
		return
	}

	logger.Info().
		Int("interval", cfg.WarmupInterval).
		Int("debounce", cfg.WarmupDebounce).
		Msg("Warmup service starting")

	// 1. Initial Warmup: identify top-2 and send immediate warmup
	top2 := wm.getTop2(cfg)
	for _, m := range top2 {
		wm.sendWarmupRequest(m.Provider.Name, m.Model)
	}

	lastTop2 := make(map[string]bool)
	for _, m := range top2 {
		lastTop2[makeKey(m.Provider.Name, m.Model)] = true
	}

	ticker := time.NewTicker(time.Duration(cfg.WarmupInterval) * time.Second)
	defer ticker.Stop()

	debounceTimers := make(map[string]*time.Timer)

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("Warmup service stopping (context canceled)")
			return
		case <-wm.stop:
			logger.Info().Msg("Warmup service stopping")
			return
		case _, ok := <-StatusEvents:
			if !ok {
				return
			}
			lastTop2 = wm.handleStatusChange(lastTop2, debounceTimers)
		case <-ticker.C:
			wm.handlePeriodicWarmup()
		}
	}
}

func (wm *WarmupManager) handleStatusChange(lastTop2 map[string]bool, debounceTimers map[string]*time.Timer) map[string]bool {
	currentCfg := config.GetConfig()
	if !currentCfg.WarmupEnabled {
		return lastTop2
	}

	currentTop2 := wm.getTop2(currentCfg)
	currentKeys := make(map[string]bool)

	for _, m := range currentTop2 {
		key := makeKey(m.Provider.Name, m.Model)
		currentKeys[key] = true

		// Debounce logic: when a new model enters Top-2, wait WarmupDebounce (60s) before first warmup
		if !lastTop2[key] {
			logger.Debug().
				Str("provider", m.Provider.Name).
				Str("model", m.Model).
				Msg("New model in Top-2, starting debounce timer")

			if timer, ok := debounceTimers[key]; ok {
				timer.Stop()
			}

			pName := m.Provider.Name
			mName := m.Model
			debounceTimers[key] = time.AfterFunc(time.Duration(currentCfg.WarmupDebounce)*time.Second, func() {
				wm.sendWarmupRequest(pName, mName)
			})
		}
	}

	// Clean up timers for models no longer in top 2
	for key, timer := range debounceTimers {
		if !currentKeys[key] {
			timer.Stop()
			delete(debounceTimers, key)
		}
	}

	return currentKeys
}

func (wm *WarmupManager) handlePeriodicWarmup() {
	currentCfg := config.GetConfig()
	if !currentCfg.WarmupEnabled {
		return
	}

	top2 := wm.getTop2(currentCfg)
	for _, m := range top2 {
		wm.sendWarmupRequest(m.Provider.Name, m.Model)
	}
}

// Stop signals the manager to stop
func (wm *WarmupManager) Stop() {
	close(wm.stop)
}

func (wm *WarmupManager) getTop2(cfg config.Config) []ProviderModelPair {
	// Reusing getCandidates with dummy "auto" request
	candidates := getCandidates(cfg, "auto", "")
	if len(candidates) > 2 {
		return candidates[:2]
	}
	return candidates
}

func (wm *WarmupManager) sendWarmupRequest(provider, model string) {
	key := makeKey(provider, model)

	wm.mu.Lock()
	if wm.inFlight[key] {
		wm.mu.Unlock()
		return
	}
	wm.inFlight[key] = true
	wm.mu.Unlock()

	defer func() {
		wm.mu.Lock()
		delete(wm.inFlight, key)
		wm.mu.Unlock()
	}()

	logger.Debug().
		Str("provider", provider).
		Str("model", model).
		Msg("Sending warmup request")

	req := Request{
		Model:    model,
		Provider: provider,
		Messages: []Message{
			{Role: "user", Content: "hi"},
		},
		MaxTokens: 1,
		IsWarmup:  true,
	}

	// Internal call to Proxy
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, _, err := ProxyFunc(ctx, req)
	if err != nil {
		logger.Warn().
			Err(err).
			Str("provider", provider).
			Str("model", model).
			Msg("Warmup request failed")
	} else {
		logger.Debug().
			Str("provider", provider).
			Str("model", model).
			Msg("Warmup request succeeded")
	}
}
