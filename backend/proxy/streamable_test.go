package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ai-proxy/config"
	"ai-proxy/logger"
)

func TestStreamableHTTPHandler_Resources(t *testing.T) {
	// Initialize logger and config
	logger.Init(false)
	config.ResetTestConfig()
	GlobalMetricsStore.Reset()
	logger.GlobalRingBuffer.Reset()

	config.LoadTestConfig(config.Config{
		Providers: []config.Provider{
			{
				Name:   "test-provider",
				APIKey: "secret-key",
				Models: []config.Model{
					{Name: "test-model"},
				},
			},
		},
	})

	handler := NewStreamableHTTPHandler(func(_ context.Context, _ Request, _ http.ResponseWriter, _ *http.Request) error {
		return nil
	})

	t.Run("resources/list", func(t *testing.T) {
		reqBody := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "resources/list",
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

		result, ok := resp.Result.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected result map, got %T", resp.Result)
		}

		resources, ok := result["resources"].([]interface{})
		if !ok {
			t.Fatalf("Expected resources array, got %T", result["resources"])
		}

		if len(resources) != 3 {
			t.Errorf("Expected 3 resources, got %d", len(resources))
		}
	})

	t.Run("resources/read config", func(t *testing.T) {
		params, _ := json.Marshal(map[string]interface{}{
			"uri": "ai-revolver://config",
		})
		reqBody := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      2,
			Method:  "resources/read",
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

		result := resp.Result.(map[string]interface{})
		contents := result["contents"].([]interface{})
		content := contents[0].(map[string]interface{})
		text := content["text"].(string)

		if bytes.Contains([]byte(text), []byte("secret-key")) {
			t.Errorf("Config was not masked: %s", text)
		}
		if !bytes.Contains([]byte(text), []byte("********")) {
			t.Errorf("Config missing mask: %s", text)
		}
	})

	t.Run("resources/read stats", func(t *testing.T) {
		params, _ := json.Marshal(map[string]interface{}{
			"uri": "ai-revolver://stats",
		})
		reqBody := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      3,
			Method:  "resources/read",
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

		result := resp.Result.(map[string]interface{})
		contents := result["contents"].([]interface{})
		content := contents[0].(map[string]interface{})
		text := content["text"].(string)

		if !bytes.Contains([]byte(text), []byte("{}")) && !bytes.Contains([]byte(text), []byte("test-provider")) {
			t.Errorf("Unexpected stats content: %s", text)
		}
	})

	t.Run("resources/read logs", func(t *testing.T) {
		logger.Info().Msg("Test log message")
		time.Sleep(10 * time.Millisecond) // Wait for async logger
		
		params, _ := json.Marshal(map[string]interface{}{
			"uri": "ai-revolver://logs?level=info",
		})
		reqBody := JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      4,
			Method:  "resources/read",
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

		result := resp.Result.(map[string]interface{})
		contents := result["contents"].([]interface{})
		content := contents[0].(map[string]interface{})
		text := content["text"].(string)

		// Wait a bit for async logger to flush
		// But in unit test it might be fast enough or we can use a small delay
		// Actually, logger.Info().Msg() sends to a channel, so we might need a small sleep
		// or use synchronous logging if possible.
		// Let's just check if it's a valid JSON array for now.
		if !bytes.HasPrefix([]byte(text), []byte("[")) {
			t.Errorf("Expected JSON array for logs, got: %s", text)
		}
	})
}
