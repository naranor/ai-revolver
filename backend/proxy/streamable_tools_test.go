package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"ai-proxy/config"
	"ai-proxy/logger"
)

func TestStreamableHTTPHandler_Tools(t *testing.T) {
	// Initialize logger and config
	logger.Init(false)
	config.ResetTestConfig()

	// Setup config file for update_config test
	tmpDir, _ := os.MkdirTemp("", "mcp_test")
	defer os.RemoveAll(tmpDir)
	cfgPath := filepath.Join(tmpDir, "config.json")
	initialConfig := config.Config{
		Providers: []config.Provider{
			{
				Name:   "test-provider",
				APIKey: "key1",
				Models: []config.Model{
					{Name: "test-model"},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(initialConfig, "", "  ")
	os.WriteFile(cfgPath, data, 0644)
	config.LoadConfig(cfgPath)

	handler := NewStreamableHTTPHandler()

	t.Run("tools/list", func(t *testing.T) {
		reqBody := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "tools/list",
		}
		data, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBuffer(data))
		w := httptest.NewRecorder()

		session := handler.sessions.CreateSession("test")
		req.Header.Set(HeaderMCPSessionID, session.ID)

		handler.Handle(w, req)

		var resp JSONRPCResponse
		json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Error != nil {
			t.Fatalf("Expected no error, got %v", resp.Error)
		}

		result := resp.Result.(map[string]interface{})
		tools := result["tools"].([]interface{})

		// chat_completion removed; config/stats/ops tools remain
		if len(tools) != 23 {
			t.Errorf("Expected 23 tools, got %d", len(tools))
		}
		for _, raw := range tools {
			tool := raw.(map[string]interface{})
			if tool["name"] == "chat_completion" {
				t.Fatal("chat_completion must not be listed")
			}
		}
	})

	t.Run("tools/call chat_completion rejected", func(t *testing.T) {
		params, _ := json.Marshal(map[string]interface{}{
			"name": "chat_completion",
			"arguments": map[string]interface{}{
				"messages": []map[string]interface{}{
					{"role": "user", "content": "hi"},
				},
			},
		})
		reqBody := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      99,
			Method:  "tools/call",
			Params:  params,
		}
		data, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBuffer(data))
		w := httptest.NewRecorder()

		session := handler.sessions.CreateSession("test")
		req.Header.Set(HeaderMCPSessionID, session.ID)

		handler.Handle(w, req)

		var resp JSONRPCResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Error == nil {
			t.Fatal("Expected error for removed chat_completion tool")
		}
	})

	t.Run("tools/call update_config", func(t *testing.T) {
		params, _ := json.Marshal(map[string]interface{}{
			"name": "update_config",
			"arguments": map[string]interface{}{
				"patch": []map[string]interface{}{
					{
						"op":    "replace",
						"path":  "/providers/0/name",
						"value": "updated-provider",
					},
				},
			},
		})
		reqBody := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      2,
			Method:  "tools/call",
			Params:  params,
		}
		data, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBuffer(data))
		w := httptest.NewRecorder()

		session := handler.sessions.CreateSession("test")
		req.Header.Set(HeaderMCPSessionID, session.ID)

		handler.Handle(w, req)

		var resp JSONRPCResponse
		json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Error != nil {
			t.Fatalf("Expected no error, got %v", resp.Error)
		}

		// Verify change
		cfg := config.GetConfig()
		if cfg.Providers[0].Name != "updated-provider" {
			t.Errorf("Expected updated-provider, got %s", cfg.Providers[0].Name)
		}
	})

	t.Run("tools/call test_provider fail", func(t *testing.T) {
		// Mock forwardRequest failure since we don't have a real server
		// Wait, test_provider calls forwardRequest which will try to connect.
		// For this test, we can provide a non-existent provider.
		params, _ := json.Marshal(map[string]interface{}{
			"name": "test_provider",
			"arguments": map[string]interface{}{
				"provider": "non-existent",
			},
		})
		reqBody := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      3,
			Method:  "tools/call",
			Params:  params,
		}
		data, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBuffer(data))
		w := httptest.NewRecorder()

		session := handler.sessions.CreateSession("test")
		req.Header.Set(HeaderMCPSessionID, session.ID)

		handler.Handle(w, req)

		var resp JSONRPCResponse
		json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.Error == nil {
			t.Error("Expected error for non-existent provider, got nil")
		} else if resp.Error.Code != -32602 {
			t.Errorf("Expected code -32602, got %d", resp.Error.Code)
		}
	})
}
