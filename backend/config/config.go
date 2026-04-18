// Package config provides configuration management for the AI Proxy
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	jsonpatch "github.com/evanphx/json-patch/v5"
)

// Provider represents an AI model provider configuration
type Provider struct {
	Enabled      *bool   `json:"enabled,omitempty"`
	Name         string  `json:"name"`
	APIKey       string  `json:"api_key"`
	BaseURL      string  `json:"base_url"`
	Models       []Model `json:"models"`
	RateLimit    int     `json:"rate_limit"`
	CurrentUsage int     `json:"current_usage"`
	Priority     int     `json:"priority"`
}

// IsEnabled returns true if provider is enabled (defaults to true if not set)
func (p *Provider) IsEnabled() bool {
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
}

// IsNativeOllama returns true if the provider is Ollama and uses native API (not /v1)
func (p *Provider) IsNativeOllama() bool {
	if p.Name != "ollama" {
		return false
	}
	// If URL contains /v1, it's OpenAI-compatible mode
	return !strings.Contains(p.BaseURL, "/v1")
}

// Model represents a specific AI model configuration
type Model struct {
	Name      string `json:"name"`
	MaxTokens int    `json:"max_tokens"`
	Thinking  bool   `json:"thinking"`
	Reasoning bool   `json:"reasoning"`
	Tools     bool   `json:"tools"`
}

// AutoMode defines settings for automatic provider/model selection
type AutoMode struct {
	FallbackStrategy string `json:"fallback_strategy"`
	Enabled          bool   `json:"enabled"`
}

// Config is the root configuration structure
type Config struct {
	AutoMode               AutoMode   `json:"auto_mode"`
	Providers              []Provider `json:"providers"`
	WarmupEnabled          bool       `json:"warmup_enabled"`
	WarmupInterval         int        `json:"warmup_interval"`
	WarmupDebounce         int        `json:"warmup_debounce"`
	MaxRetries             int        `json:"max_retries"`
	ConnectTimeoutSeconds  int        `json:"connect_timeout_seconds"`
	ResponseTimeoutSeconds int        `json:"response_timeout_seconds"`
}

// GetConnectTimeout returns the connection timeout duration
func (c *Config) GetConnectTimeout() time.Duration {
	if c.ConnectTimeoutSeconds <= 0 {
		return 5 * time.Second
	}
	return time.Duration(c.ConnectTimeoutSeconds) * time.Second
}

// GetResponseTimeout returns the response timeout duration
func (c *Config) GetResponseTimeout() time.Duration {
	if c.ResponseTimeoutSeconds <= 0 {
		return 300 * time.Second
	}
	return time.Duration(c.ResponseTimeoutSeconds) * time.Second
}

var (
	globalConfig Config
	mu           sync.RWMutex
	configPath   string
	writeFile    = os.WriteFile
	renameFile   = os.Rename
)

// LoadConfig reads configuration from a file
func LoadConfig(path string) error {
	config := Config{}
	data, err := os.ReadFile(path) //nolint:gosec // path is intentionally a variable to allow different config locations
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	globalConfig = config
	configPath = path
	return nil
}

// GetConfig returns the current global configuration
var GetConfig = func() Config {
	mu.RLock()
	defer mu.RUnlock()
	cfg := globalConfig
	if cfg.WarmupInterval == 0 {
		cfg.WarmupInterval = 180
		// If Interval was 0, we assume it's missing and default Enabled to true
		cfg.WarmupEnabled = true
	}
	if cfg.WarmupDebounce == 0 {
		cfg.WarmupDebounce = 60
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 5
	}
	return cfg
}

// GetMaskedConfig returns configuration with API keys masked
func GetMaskedConfig() Config {
	mu.RLock()
	defer mu.RUnlock()

	// Deep copy
	cfg := globalConfig
	cfg.Providers = make([]Provider, len(globalConfig.Providers))
	for i, p := range globalConfig.Providers {
		pCopy := p
		pCopy.APIKey = "********"
		if p.Models != nil {
			pCopy.Models = make([]Model, len(p.Models))
			copy(pCopy.Models, p.Models)
		}
		cfg.Providers[i] = pCopy
	}
	return cfg
}

// LoadTestConfig injects a configuration for testing
func LoadTestConfig(cfg Config) {
	mu.Lock()
	defer mu.Unlock()
	globalConfig = cfg
}

// ReloadConfig reloads configuration from the same path
var ReloadConfig = func(path string) error {
	return LoadConfig(path)
}

// ResetTestConfig resets the global config to zero value (useful for tests)
func ResetTestConfig() {
	mu.Lock()
	defer mu.Unlock()
	globalConfig = Config{}
}

// SetConfigPath sets the path to the configuration file
func SetConfigPath(path string) {
	mu.Lock()
	defer mu.Unlock()
	configPath = path
}

// backupConfig creates a timestamped backup of the current configuration file
func backupConfig() error {
	if configPath == "" {
		return nil // Nothing to backup
	}

	// 1. Read current config file
	//nolint:gosec // configPath is controlled internally
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file yet, no need to backup
		}
		return fmt.Errorf("failed to read config for backup: %w", err)
	}

	// 2. Ensure backups directory exists
	configDir := filepath.Dir(configPath)
	backupDir := filepath.Join(configDir, "backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// 3. Create backup filename with timestamp
	timestamp := time.Now().Format("20060102-150405")
	backupFileName := fmt.Sprintf("config-%s.json", timestamp)
	backupPath := filepath.Join(backupDir, backupFileName)

	// 4. Write backup file
	if err := os.WriteFile(backupPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write backup file: %w", err)
	}

	return nil
}

// saveConfig persists a Config object to the configuration file and updates the global state.
// Caller MUST hold the lock.
func saveConfig(newConfig Config) error {
	if configPath == "" {
		return fmt.Errorf("config path not set")
	}

	// 1. Atomic Write to File
	tmpFile := configPath + ".tmp"
	data, marshalErr := json.MarshalIndent(newConfig, "", "  ")
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal new config: %w", marshalErr)
	}

	if err := writeFile(tmpFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write temporary config file: %w", err)
	}

	// 2. Rename temporary file to actual config file
	if err := renameFile(tmpFile, configPath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to rename temporary config file: %w", err)
	}

	// 3. Update globalConfig in memory
	globalConfig = newConfig
	return nil
}

// UpdateConfig applies a JSON patch to the current configuration OR replaces it entirely, then saves it atomically.
var UpdateConfig = func(data []byte) error {
	mu.Lock()
	defer mu.Unlock()

	if configPath == "" {
		return fmt.Errorf("config path not set")
	}

	// 0. Backup current config before modification
	if err := backupConfig(); err != nil {
		// Log error but continue with update - backup failure shouldn't block updates
		// but in this system we might want to know. For now, we return error to be safe.
		return fmt.Errorf("backup failed: %w", err)
	}

	// 1. Try to unmarshal as a full Config object first
	var newConfig Config
	if err := json.Unmarshal(data, &newConfig); err == nil {
		// If it's a valid Config object (e.g. from UI), save it
		// We use a simple heuristic: if it has Providers, it's likely a full config.
		// jsonpatch.Patch is an array, which will fail unmarshaling into a struct.
		return saveConfig(newConfig)
	}

	// 2. If it's not a full Config, try to apply it as a JSON patch
	currentJSON, err := json.Marshal(globalConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal current config: %w", err)
	}

	patch, err := jsonpatch.DecodePatch(data)
	if err != nil {
		return fmt.Errorf("failed to decode patch (and not a valid full config): %w", err)
	}

	modifiedJSON, err := patch.Apply(currentJSON)
	if err != nil {
		return fmt.Errorf("failed to apply patch: %w", err)
	}

	// 3. Unmarshal back to Config to validate
	if unmarshalErr := json.Unmarshal(modifiedJSON, &newConfig); unmarshalErr != nil {
		return fmt.Errorf("failed to unmarshal modified config: %w", unmarshalErr)
	}

	// 4. Save the modified config
	return saveConfig(newConfig)
}
