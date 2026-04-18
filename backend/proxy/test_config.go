package proxy

import (
	"ai-proxy/config"
	"time"
)

// ResetTestState resets all failure tracking and last successful model for clean test state
func ResetTestState() {
	ResetAllFailures()
	ClearLastSuccessfulModel()
}

// BoolPtr returns a pointer to a bool value
func BoolPtr(b bool) *bool {
	return &b
}

// TestConfig creates a test config with a mock URL for all providers.
// Note: All providers share the same URL, so the mock can't distinguish between them.
// For provider-specific testing, use TestConfigWithMultiMock.
func TestConfig(mockURL string) config.Config {
	ResetTestState()
	return config.Config{
		Providers: []config.Provider{
			{
				Name:      "fast",
				APIKey:    "test-key",
				BaseURL:   mockURL,
				Priority:  1,
				RateLimit: 100,
				Enabled:   BoolPtr(true),
				Models: []config.Model{
					{Name: "model-a"},
				},
			},
			{
				Name:      "medium",
				APIKey:    "test-key",
				BaseURL:   mockURL,
				Priority:  2,
				RateLimit: 100,
				Enabled:   BoolPtr(true),
				Models: []config.Model{
					{Name: "model-b"},
				},
			},
			{
				Name:      "slow",
				APIKey:    "test-key",
				BaseURL:   mockURL,
				Priority:  3,
				RateLimit: 100,
				Enabled:   BoolPtr(true),
				Models: []config.Model{
					{Name: "model-c"},
				},
			},
		},
		AutoMode: config.AutoMode{
			Enabled: true,
		},
	}
}

// TestConfigWithMultiMock creates a test config using MultiMockServer for proper provider isolation.
func TestConfigWithMultiMock(multiMock *MultiMockServer) config.Config {
	ResetTestState()
	return config.Config{
		Providers: []config.Provider{
			{
				Name:      "fast",
				APIKey:    "test-key",
				BaseURL:   multiMock.URLFor("fast"),
				Priority:  1,
				RateLimit: 100,
				Enabled:   BoolPtr(true),
				Models: []config.Model{
					{Name: "model-a"},
				},
			},
			{
				Name:      "medium",
				APIKey:    "test-key",
				BaseURL:   multiMock.URLFor("medium"),
				Priority:  2,
				RateLimit: 100,
				Enabled:   BoolPtr(true),
				Models: []config.Model{
					{Name: "model-b"},
				},
			},
			{
				Name:      "slow",
				APIKey:    "test-key",
				BaseURL:   multiMock.URLFor("slow"),
				Priority:  3,
				RateLimit: 100,
				Enabled:   BoolPtr(true),
				Models: []config.Model{
					{Name: "model-c"},
				},
			},
		},
		AutoMode: config.AutoMode{
			Enabled: true,
		},
	}
}

// TestConfigMultiModel создаёт конфиг с несколькими моделями на провайдера
func TestConfigMultiModel(mockURL string) config.Config {
	return config.Config{
		Providers: []config.Provider{
			{
				Name:     "provider1",
				APIKey:   "test-key",
				BaseURL:  mockURL,
				Priority: 1,
				Enabled:  BoolPtr(true),
				Models: []config.Model{
					{Name: "model-1"},
					{Name: "model-2"},
					{Name: "model-3"},
				},
			},
			{
				Name:     "provider2",
				APIKey:   "test-key",
				BaseURL:  mockURL,
				Priority: 2,
				Enabled:  BoolPtr(true),
				Models: []config.Model{
					{Name: "model-1"},
					{Name: "model-2"},
				},
			},
		},
		AutoMode: config.AutoMode{
			Enabled: true,
		},
	}
}

// TestConfigWithDuplicates создаёт конфиг с одинаковыми моделями у разных провайдеров
func TestConfigWithDuplicates(mockURL string) config.Config {
	return config.Config{
		Providers: []config.Provider{
			{
				Name:     "prov1",
				APIKey:   "test-key",
				BaseURL:  mockURL,
				Priority: 1,
				Enabled:  BoolPtr(true),
				Models: []config.Model{
					{Name: "shared-model"},
				},
			},
			{
				Name:     "prov2",
				APIKey:   "test-key",
				BaseURL:  mockURL,
				Priority: 2,
				Enabled:  BoolPtr(true),
				Models: []config.Model{
					{Name: "shared-model"},
				},
			},
		},
		AutoMode: config.AutoMode{
			Enabled: true,
		},
	}
}

// TestConfigEmptyProviders создаёт пустой конфиг
func TestConfigEmptyProviders() config.Config {
	return config.Config{
		Providers: []config.Provider{},
		AutoMode: config.AutoMode{
			Enabled: true,
		},
	}
}

// TestConfigWithLatency создаёт конфиг с разными задержками
func TestConfigWithLatency(mockURL string, _ time.Duration) config.Config {
	return config.Config{
		Providers: []config.Provider{
			{
				Name:     "fast",
				APIKey:   "test-key",
				BaseURL:  mockURL,
				Priority: 1,
				Enabled:  BoolPtr(true),
				Models: []config.Model{
					{Name: "fast-model"},
				},
			},
			{
				Name:     "slow",
				APIKey:   "test-key",
				BaseURL:  mockURL,
				Priority: 2,
				Enabled:  BoolPtr(true),
				Models: []config.Model{
					{Name: "slow-model"},
				},
			},
		},
		AutoMode: config.AutoMode{
			Enabled: true,
		},
	}
}
