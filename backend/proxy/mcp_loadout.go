package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"ai-proxy/config"
)

func (h *StreamableHTTPHandler) handleAddProviderTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	name := getStringParam(args, "name")
	baseURL := getStringParam(args, "base_url")
	apiKey := getStringParam(args, "api_key")
	priority := 1
	if p, ok := args["priority"].(float64); ok {
		priority = int(p)
	}

	patch := []map[string]interface{}{
		{
			"op":   "add",
			"path": "/providers/-",
			"value": map[string]interface{}{
				"name":      name,
				"base_url":  baseURL,
				"api_key":   apiKey,
				"priority":  priority,
				"enabled":   true,
				"models":    []interface{}{},
				"rate_limit": 0,
			},
		},
	}

	patchData, _ := json.Marshal(patch)
	if err := config.UpdateConfig(patchData); err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", err.Error())
		return
	}

	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": fmt.Sprintf("Provider '%s' added.", name)}},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleRemoveProviderTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	name := getStringParam(args, "name")
	cfg := config.GetConfig()
	index := -1
	for i, p := range cfg.Providers {
		if p.Name == name {
			index = i
			break
		}
	}

	if index == -1 {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "Provider not found")
		return
	}

	patch := []map[string]interface{}{
		{
			"op":   "remove",
			"path": fmt.Sprintf("/providers/%d", index),
		},
	}

	patchData, _ := json.Marshal(patch)
	if err := config.UpdateConfig(patchData); err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", err.Error())
		return
	}

	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": fmt.Sprintf("Provider '%s' removed.", name)}},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleUpdateProviderTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	name := getStringParam(args, "name")
	cfg := config.GetConfig()
	index := -1
	for i, p := range cfg.Providers {
		if p.Name == name {
			index = i
			break
		}
	}

	if index == -1 {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "Provider not found")
		return
	}

	var patches []map[string]interface{}
	if val, ok := args["enabled"].(bool); ok {
		patches = append(patches, map[string]interface{}{"op": "replace", "path": fmt.Sprintf("/providers/%d/enabled", index), "value": val})
	}
	if val := getStringParam(args, "api_key"); val != "" {
		patches = append(patches, map[string]interface{}{"op": "replace", "path": fmt.Sprintf("/providers/%d/api_key", index), "value": val})
	}
	if val := getStringParam(args, "base_url"); val != "" {
		patches = append(patches, map[string]interface{}{"op": "replace", "path": fmt.Sprintf("/providers/%d/base_url", index), "value": val})
	}
	if val, ok := args["priority"].(float64); ok {
		patches = append(patches, map[string]interface{}{"op": "replace", "path": fmt.Sprintf("/providers/%d/priority", index), "value": int(val)})
	}

	if len(patches) == 0 {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "No updates provided")
		return
	}

	patchData, _ := json.Marshal(patches)
	if err := config.UpdateConfig(patchData); err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", err.Error())
		return
	}

	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": fmt.Sprintf("Provider '%s' updated.", name)}},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleAddModelTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	providerName := getStringParam(args, "provider")
	name := getStringParam(args, "name")
	maxTokens := 4096
	if mt, ok := args["max_tokens"].(float64); ok {
		maxTokens = int(mt)
	}

	cfg := config.GetConfig()
	pIndex := -1
	for i, p := range cfg.Providers {
		if p.Name == providerName {
			pIndex = i
			break
		}
	}

	if pIndex == -1 {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "Provider not found")
		return
	}

	patch := []map[string]interface{}{
		{
			"op":   "add",
			"path": fmt.Sprintf("/providers/%d/models/-", pIndex),
			"value": map[string]interface{}{
				"name":       name,
				"max_tokens": maxTokens,
				"thinking":   false,
				"reasoning":  false,
				"tools":      false,
			},
		},
	}

	patchData, _ := json.Marshal(patch)
	if err := config.UpdateConfig(patchData); err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", err.Error())
		return
	}

	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": fmt.Sprintf("Model '%s' added to provider '%s'.", name, providerName)}},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleRemoveModelTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	providerName := getStringParam(args, "provider")
	modelName := getStringParam(args, "model")

	cfg := config.GetConfig()
	pIndex, mIndex := -1, -1
	for i, p := range cfg.Providers {
		if p.Name == providerName {
			pIndex = i
			for j, m := range p.Models {
				if m.Name == modelName {
					mIndex = j
					break
				}
			}
			break
		}
	}

	if pIndex == -1 || mIndex == -1 {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "Provider or model not found")
		return
	}

	patch := []map[string]interface{}{
		{
			"op":   "remove",
			"path": fmt.Sprintf("/providers/%d/models/%d", pIndex, mIndex),
		},
	}

	patchData, _ := json.Marshal(patch)
	if err := config.UpdateConfig(patchData); err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", err.Error())
		return
	}

	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": fmt.Sprintf("Model '%s' removed from provider '%s'.", modelName, providerName)}},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleUpdateModelTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	providerName := getStringParam(args, "provider")
	modelName := getStringParam(args, "model")

	cfg := config.GetConfig()
	pIndex, mIndex := -1, -1
	for i, p := range cfg.Providers {
		if p.Name == providerName {
			pIndex = i
			for j, m := range p.Models {
				if m.Name == modelName {
					mIndex = j
					break
				}
			}
			break
		}
	}

	if pIndex == -1 || mIndex == -1 {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "Provider or model not found")
		return
	}

	var patches []map[string]interface{}
	if val := getStringParam(args, "name"); val != "" {
		patches = append(patches, map[string]interface{}{"op": "replace", "path": fmt.Sprintf("/providers/%d/models/%d/name", pIndex, mIndex), "value": val})
	}
	if val, ok := args["max_tokens"].(float64); ok {
		patches = append(patches, map[string]interface{}{"op": "replace", "path": fmt.Sprintf("/providers/%d/models/%d/max_tokens", pIndex, mIndex), "value": int(val)})
	}
	if val, ok := args["thinking"].(bool); ok {
		patches = append(patches, map[string]interface{}{"op": "replace", "path": fmt.Sprintf("/providers/%d/models/%d/thinking", pIndex, mIndex), "value": val})
	}
	if val, ok := args["reasoning"].(bool); ok {
		patches = append(patches, map[string]interface{}{"op": "replace", "path": fmt.Sprintf("/providers/%d/models/%d/reasoning", pIndex, mIndex), "value": val})
	}
	if val, ok := args["tools"].(bool); ok {
		patches = append(patches, map[string]interface{}{"op": "replace", "path": fmt.Sprintf("/providers/%d/models/%d/tools", pIndex, mIndex), "value": val})
	}

	if len(patches) == 0 {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "No updates provided")
		return
	}

	patchData, _ := json.Marshal(patches)
	if err := config.UpdateConfig(patchData); err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", err.Error())
		return
	}

	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": fmt.Sprintf("Model '%s' in provider '%s' updated.", modelName, providerName)}},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleProbeModelTool(ctx context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	providerName := getStringParam(args, "provider")
	modelName := getStringParam(args, "model")

	cfg := config.GetConfig()
	var targetProvider *config.Provider
	for _, p := range cfg.Providers {
		if p.Name == providerName {
			targetProvider = &p
			break
		}
	}

	if targetProvider == nil {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "Provider not found")
		return
	}

	// Tool use probe payload
	probeReq := Request{
		Model: modelName,
		Messages: []Message{
			{Role: "user", Content: "What is the weather in London? Call the get_weather function to answer."},
		},
		Tools: []interface{}{
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "get_weather",
					"description": "Get current weather",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"location": map[string]interface{}{"type": "string"},
						},
					},
				},
			},
		},
		ToolChoice: "auto",
	}

	startTime := time.Now()
	result, code, err := forwardRequest(ctx, *targetProvider, modelName, probeReq)
	latency := time.Since(startTime).Milliseconds()

	hasToolCalls := false
	if err == nil && len(result.Choices) > 0 {
		msg := result.Choices[0].Message
		hasToolCalls = msg.ToolCalls != nil
	}

	status := "FAIL"
	if err == nil {
		if hasToolCalls {
			status = "HIT"
		} else {
			status = "NO TOOLS"
		}
	}

	text := fmt.Sprintf("Probe Result for %s/%s:\nStatus: %s\nLatency: %dms\nCode: %d\nError: %v", providerName, modelName, status, latency, code, err)

	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": text}},
		"isError": err != nil || !hasToolCalls,
	})
}

func (h *StreamableHTTPHandler) handleProbeAllModelsTool(ctx context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	providerName := getStringParam(args, "provider")
	cfg := config.GetConfig()
	var targetProvider *config.Provider
	for _, p := range cfg.Providers {
		if p.Name == providerName {
			targetProvider = &p
			break
		}
	}

	if targetProvider == nil {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "Provider not found")
		return
	}

	results := ""
	for _, m := range targetProvider.Models {
		probeReq := Request{
			Model: m.Name,
			Messages: []Message{
				{Role: "user", Content: "ping"},
			},
			MaxTokens: 1,
		}
		_, code, err := forwardRequest(ctx, *targetProvider, m.Name, probeReq)
		status := "OK"
		if err != nil {
			status = fmt.Sprintf("FAIL (%d)", code)
		}
		results += fmt.Sprintf("- %s: %s\n", m.Name, status)
	}

	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": "Probe All Results:\n" + results}},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleOptimizeProviderTool(ctx context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	providerName := getStringParam(args, "provider")
	cfg := config.GetConfig()
	pIndex := -1
	for i, p := range cfg.Providers {
		if p.Name == providerName {
			pIndex = i
			break
		}
	}

	if pIndex == -1 {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "Provider not found")
		return
	}

	provider := cfg.Providers[pIndex]
	type modelLatency struct {
		model   config.Model
		latency int64
	}
	results := make([]modelLatency, 0, len(provider.Models))

	for _, m := range provider.Models {
		startTime := time.Now()
		probeReq := Request{
			Model: m.Name,
			Messages: []Message{
				{Role: "user", Content: "hi"},
			},
			MaxTokens: 1,
		}
		_, _, err := forwardRequest(ctx, provider, m.Name, probeReq)
		latency := int64(999999)
		if err == nil {
			latency = time.Since(startTime).Milliseconds()
		}
		results = append(results, modelLatency{model: m, latency: latency})
	}

	// Sort by latency
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[i].latency > results[j].latency {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	sortedModels := make([]config.Model, 0, len(results))
	for _, r := range results {
		sortedModels = append(sortedModels, r.model)
	}

	patch := []map[string]interface{}{
		{
			"op":    "replace",
			"path":  fmt.Sprintf("/providers/%d/models", pIndex),
			"value": sortedModels,
		},
	}

	patchData, _ := json.Marshal(patch)
	if err := config.UpdateConfig(patchData); err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", err.Error())
		return
	}

	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": fmt.Sprintf("Provider '%s' optimized. Models sorted by latency.", providerName)}},
		"isError": false,
	})
}
