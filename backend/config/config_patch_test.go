package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateConfig(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "config_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfgPath := filepath.Join(tmpDir, "config.json")
	initialConfig := Config{
		Providers: []Provider{
			{
				Name:     "test-provider",
				APIKey:   "key1",
				Priority: 1,
			},
		},
		AutoMode: AutoMode{
			Enabled: true,
		},
	}
	data, _ := json.MarshalIndent(initialConfig, "", "  ")
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	if err := LoadConfig(cfgPath); err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	t.Run("UpdatePriority", func(t *testing.T) {
		patch := []byte(`[{"op": "replace", "path": "/providers/0/priority", "value": 10}]`)
		if err := UpdateConfig(patch); err != nil {
			t.Fatalf("UpdateConfig failed: %v", err)
		}

		cfg := GetConfig()
		if cfg.Providers[0].Priority != 10 {
			t.Errorf("Expected priority 10, got %d", cfg.Providers[0].Priority)
		}

		// Verify file content
		fileData, _ := os.ReadFile(cfgPath)
		var savedCfg Config
		json.Unmarshal(fileData, &savedCfg)
		if savedCfg.Providers[0].Priority != 10 {
			t.Errorf("File: Expected priority 10, got %d", savedCfg.Providers[0].Priority)
		}
	})

	t.Run("AddProvider", func(t *testing.T) {
		patch := []byte(`[{"op": "add", "path": "/providers/-", "value": {"name": "new-provider", "api_key": "key2", "priority": 5}}]`)
		if err := UpdateConfig(patch); err != nil {
			t.Fatalf("UpdateConfig failed: %v", err)
		}

		cfg := GetConfig()
		if len(cfg.Providers) != 2 {
			t.Errorf("Expected 2 providers, got %d", len(cfg.Providers))
		}
		if cfg.Providers[1].Name != "new-provider" {
			t.Errorf("Expected new-provider, got %s", cfg.Providers[1].Name)
		}
	})

	t.Run("FullConfigReplace", func(t *testing.T) {
		newConfig := Config{
			Providers: []Provider{
				{
					Name:     "replaced-provider",
					APIKey:   "replaced-key",
					Priority: 99,
				},
			},
			AutoMode: AutoMode{
				Enabled: false,
			},
		}
		data, _ := json.Marshal(newConfig)
		if err := UpdateConfig(data); err != nil {
			t.Fatalf("UpdateConfig failed with full config: %v", err)
		}

		cfg := GetConfig()
		if len(cfg.Providers) != 1 || cfg.Providers[0].Name != "replaced-provider" {
			t.Errorf("Expected replaced-provider, got %v", cfg.Providers)
		}
		if cfg.AutoMode.Enabled {
			t.Error("Expected AutoMode to be disabled")
		}

		// Verify file content
		fileData, _ := os.ReadFile(cfgPath)
		var savedCfg Config
		json.Unmarshal(fileData, &savedCfg)
		if savedCfg.Providers[0].Priority != 99 {
			t.Errorf("File: Expected priority 99, got %d", savedCfg.Providers[0].Priority)
		}
	})

	t.Run("InvalidPatch", func(t *testing.T) {
		patch := []byte(`[{"op": "invalid", "path": "/priority", "value": 10}]`)
		if err := UpdateConfig(patch); err == nil {
			t.Error("Expected error for invalid patch, got nil")
		}
	})

	t.Run("TypeMismatch", func(t *testing.T) {
		// Priority is int, trying to set string
		patch := []byte(`[{"op": "replace", "path": "/providers/0/priority", "value": "high"}]`)
		if err := UpdateConfig(patch); err == nil {
			t.Error("Expected error for type mismatch, got nil")
		}
	})
}
