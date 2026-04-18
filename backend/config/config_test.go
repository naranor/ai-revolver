package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()
	os.Exit(code)
}

func TestLoadConfig(t *testing.T) {
	// Create temp config file
	dir := t.TempDir()
	path := filepath.Join(dir, "test_config.json")

	cfg := Config{
		Providers: []Provider{
			{
				Name:      "test-provider",
				APIKey:    "test-key",
				BaseURL:   "http://localhost:8080",
				Priority:  1,
				RateLimit: 100,
				Models: []Model{
					{Name: "model-1", MaxTokens: 1000, Thinking: false, Reasoning: true, Tools: true},
				},
			},
		},
		AutoMode: AutoMode{
			Enabled:          true,
			FallbackStrategy: "round-robin",
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Load config
	if err := LoadConfig(path); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify loaded config
	loaded := GetConfig()
	if len(loaded.Providers) != 1 {
		t.Fatalf("Expected 1 provider, got %d", len(loaded.Providers))
	}

	p := loaded.Providers[0]
	if p.Name != "test-provider" {
		t.Errorf("Expected provider name 'test-provider', got '%s'", p.Name)
	}
	if p.APIKey != "test-key" {
		t.Errorf("Expected API key 'test-key', got '%s'", p.APIKey)
	}
	if p.BaseURL != "http://localhost:8080" {
		t.Errorf("Expected base URL 'http://localhost:8080', got '%s'", p.BaseURL)
	}
	if p.RateLimit != 100 {
		t.Errorf("Expected rate limit 100, got %d", p.RateLimit)
	}

	if len(p.Models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(p.Models))
	}

	m := p.Models[0]
	if m.Name != "model-1" {
		t.Errorf("Expected model name 'model-1', got '%s'", m.Name)
	}
	if !m.Reasoning {
		t.Error("Expected Reasoning to be true")
	}
	if !m.Tools {
		t.Error("Expected Tools to be true")
	}

	if !loaded.AutoMode.Enabled {
		t.Error("Expected AutoMode to be enabled")
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	err := LoadConfig("/nonexistent/path/config.json")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.json")

	if err := os.WriteFile(path, []byte("invalid json{"), 0644); err != nil {
		t.Fatal(err)
	}

	err := LoadConfig(path)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestGetConfig_Empty(t *testing.T) {
	// Reset global config
	mu.Lock()
	globalConfig = Config{}
	mu.Unlock()

	cfg := GetConfig()
	if len(cfg.Providers) != 0 {
		t.Errorf("Expected 0 providers, got %d", len(cfg.Providers))
	}
	if cfg.MaxRetries != 5 {
		t.Errorf("Expected default MaxRetries to be 5, got %d", cfg.MaxRetries)
	}
}

func TestMaxRetriesLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max_retries_load.json")

	data := []byte(`{"max_retries": 10}`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := LoadConfig(path); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	cfg := GetConfig()
	if cfg.MaxRetries != 10 {
		t.Errorf("Expected loaded MaxRetries to be 10, got %d", cfg.MaxRetries)
	}
}

func TestTimeoutsLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "timeouts_load.json")

	data := []byte(`{
		"connect_timeout_seconds": 10,
		"response_timeout_seconds": 600
	}`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := LoadConfig(path); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	cfg := GetConfig()
	if cfg.ConnectTimeoutSeconds != 10 {
		t.Errorf("Expected ConnectTimeoutSeconds to be 10, got %d", cfg.ConnectTimeoutSeconds)
	}
	if cfg.ResponseTimeoutSeconds != 600 {
		t.Errorf("Expected ResponseTimeoutSeconds to be 600, got %d", cfg.ResponseTimeoutSeconds)
	}

	if cfg.GetConnectTimeout() != 10*time.Second {
		t.Errorf("Expected GetConnectTimeout to be 10s, got %v", cfg.GetConnectTimeout())
	}
	if cfg.GetResponseTimeout() != 600*time.Second {
		t.Errorf("Expected GetResponseTimeout to be 600s, got %v", cfg.GetResponseTimeout())
	}

	// Test defaults
	emptyCfg := Config{}
	if emptyCfg.GetConnectTimeout() != 5*time.Second {
		t.Errorf("Expected default GetConnectTimeout to be 5s, got %v", emptyCfg.GetConnectTimeout())
	}
	if emptyCfg.GetResponseTimeout() != 300*time.Second {
		t.Errorf("Expected default GetResponseTimeout to be 300s, got %v", emptyCfg.GetResponseTimeout())
	}
}

func TestLoadTestConfig(t *testing.T) {
	cfg := Config{
		Providers: []Provider{
			{Name: "test", Models: []Model{{Name: "model"}}},
		},
	}

	LoadTestConfig(cfg)

	loaded := GetConfig()
	if len(loaded.Providers) != 1 {
		t.Fatalf("Expected 1 provider, got %d", len(loaded.Providers))
	}
	if loaded.Providers[0].Name != "test" {
		t.Errorf("Expected provider name 'test', got '%s'", loaded.Providers[0].Name)
	}
}

func TestReloadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reload_test.json")

	// Create initial config
	cfg1 := Config{
		Providers: []Provider{{Name: "initial", Models: []Model{{Name: "model1"}}}},
	}
	data, _ := json.Marshal(cfg1)
	os.WriteFile(path, data, 0644)

	LoadConfig(path)
	if GetConfig().Providers[0].Name != "initial" {
		t.Error("Expected initial provider")
	}

	// Update config file
	cfg2 := Config{
		Providers: []Provider{{Name: "updated", Models: []Model{{Name: "model2"}}}},
	}
	data, _ = json.Marshal(cfg2)
	os.WriteFile(path, data, 0644)

	// Reload
	if err := ReloadConfig(path); err != nil {
		t.Fatalf("Failed to reload config: %v", err)
	}

	if GetConfig().Providers[0].Name != "updated" {
		t.Error("Expected updated provider after reload")
	}
}

func TestUpdateConfig_Error(t *testing.T) {
	// Test no config path
	originalConfigPath := configPath
	configPath = ""
	err := UpdateConfig([]byte(`[]`))
	if err == nil {
		t.Error("Expected an error when config path is not set")
	}
	configPath = originalConfigPath

	// Test invalid patch
	err = UpdateConfig([]byte(`invalid`))
	if err == nil {
		t.Error("Expected an error for invalid patch data")
	}

	// Test patch apply error
	err = UpdateConfig([]byte(`[{"op": "test", "path": "/a", "value": "b"}]`))
	if err == nil {
		t.Error("Expected an error when applying a patch fails")
	}
}

func TestUpdateConfig_Backup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfgPath := filepath.Join(tmpDir, "config.json")
	initialConfig := Config{
		Providers: []Provider{{Name: "initial"}},
	}
	data, _ := json.Marshal(initialConfig)
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Setup global config state
	mu.Lock()
	configPath = cfgPath
	globalConfig = initialConfig
	mu.Unlock()

	// Apply a valid patch
	patch := []byte(`[{"op": "replace", "path": "/providers/0/name", "value": "new-name"}]`)
	if err := UpdateConfig(patch); err != nil {
		t.Fatalf("UpdateConfig failed: %v", err)
	}

	// Verify backup exists
	backupDir := filepath.Join(tmpDir, "backups")
	files, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("Failed to read backup directory: %v", err)
	}

	if len(files) == 0 {
		t.Error("No backup files found")
	}

	found := false
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "config-") && strings.HasSuffix(f.Name(), ".json") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Backup file does not match expected pattern")
	}
}

func TestProvider_IsEnabled(t *testing.T) {
	bTrue := true
	bFalse := false

	tests := []struct {
		name     string
		provider Provider
		expected bool
	}{
		{"NilEnabled", Provider{}, true},
		{"TrueEnabled", Provider{Enabled: &bTrue}, true},
		{"FalseEnabled", Provider{Enabled: &bFalse}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.provider.IsEnabled(); got != tt.expected {
				t.Errorf("Provider.IsEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetMaskedConfig(t *testing.T) {
	cfg := Config{
		Providers: []Provider{
			{Name: "test", APIKey: "secret-key", Models: []Model{{Name: "model"}}},
		},
	}
	LoadTestConfig(cfg)

	masked := GetMaskedConfig()
	if len(masked.Providers) != 1 {
		t.Fatalf("Expected 1 provider, got %d", len(masked.Providers))
	}
	if masked.Providers[0].APIKey != "********" {
		t.Errorf("Expected masked API key, got '%s'", masked.Providers[0].APIKey)
	}
	// Make sure original is unchanged
	if GetConfig().Providers[0].APIKey == "********" {
		t.Error("Original config should not be modified")
	}
}

func TestResetTestConfig(t *testing.T) {
	cfg := Config{
		Providers: []Provider{
			{Name: "test"},
		},
	}
	LoadTestConfig(cfg)

	ResetTestConfig()

	if len(GetConfig().Providers) != 0 {
		t.Error("Expected config to be reset")
	}
}
