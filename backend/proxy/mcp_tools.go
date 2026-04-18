package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"ai-proxy/config"
)

func (h *StreamableHTTPHandler) handleGetActiveModelTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest) {
	provider, model, latency := GetLastSuccessfulModel()
	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Active Provider: %s\nActive Model: %s\nLast Latency: %dms", provider, model, latency),
			},
		},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleGetBlockedModelsTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest) {
	blocked := GetBlockedModels()
	data, _ := json.MarshalIndent(blocked, "", "  ")
	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": string(data),
			},
		},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleGetCircuitBreakerSettingsTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest) {
	threshold := GetLatencyThreshold()
	duration := GetBlockDuration()
	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Latency Threshold: %dms\nBlock Duration: %ds", threshold, int(duration.Seconds())),
			},
		},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleSetLatencyThresholdTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	threshold, ok := args["threshold_ms"].(float64)
	if !ok {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "threshold_ms must be an integer")
		return
	}
	SetLatencyThreshold(int64(threshold))
	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Latency threshold set to %dms", int64(threshold)),
			},
		},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleSetBlockDurationTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	duration, ok := args["duration_sec"].(float64)
	if !ok {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "duration_sec must be an integer")
		return
	}
	SetBlockDuration(time.Duration(duration) * time.Second)
	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Block duration set to %ds", int(duration)),
			},
		},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleResetFailuresTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest) {
	ResetAllFailures()
	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": "All failure counters reset and models unblocked.",
			},
		},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleClearLastModelTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest) {
	ClearLastSuccessfulModel()
	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": "Last successful model record cleared.",
			},
		},
		"isError": false,
	})
}

var (
	// FindProviderFunc is a global reference to main.FindProvider
	FindProviderFunc func(string) *config.Provider
	// BuildModelsEndpointFunc is a global reference to main.BuildModelsEndpoint
	BuildModelsEndpointFunc func(*config.Provider) string
	// FetchFromProviderFunc is a global reference to main.FetchFromProvider
	FetchFromProviderFunc func(context.Context, *config.Provider, string) (io.ReadCloser, error)
	// WriteOllamaModelsResponseFunc is a global reference to main.WriteOllamaModelsResponse
	WriteOllamaModelsResponseFunc func(http.ResponseWriter, io.ReadCloser)
)

func (h *StreamableHTTPHandler) handleScoutProviderTool(ctx context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	providerName := getStringParam(args, "provider")
	if FindProviderFunc == nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", "Scout functionality not initialized")
		return
	}

	target := FindProviderFunc(providerName)
	if target == nil {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "Provider not found")
		return
	}

	endpoint := BuildModelsEndpointFunc(target)
	body, err := FetchFromProviderFunc(ctx, target, endpoint)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Provider error", err.Error())
		return
	}

	rec := &mcpResponseRecorder{header: make(http.Header)}
	if target.IsNativeOllama() {
		WriteOllamaModelsResponseFunc(rec, body)
	} else {
		_, _ = io.Copy(&rec.body, body)
		_ = body.Close()
	}

	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": rec.body.String(),
			},
		},
		"isError": false,
	})
}

type mcpResponseRecorder struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func (r *mcpResponseRecorder) Header() http.Header { return r.header }
func (r *mcpResponseRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *mcpResponseRecorder) WriteHeader(code int) { r.code = code }
