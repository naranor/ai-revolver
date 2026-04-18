package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"ai-proxy/config"
	"ai-proxy/logger"

	"github.com/rs/zerolog"
)

// Streamable HTTP transport constants
const (
	HeaderMCPSessionID   = "Mcp-Session-Id"
	HeaderMCPProtocolVer = "Mcp-Protocol-Version"
	ContentTypeJSON      = "application/json"
	ContentTypeSSE       = "text/event-stream"
	maxRequestBodySize   = 1 << 20 // 1MB for MCP requests
)

// JSONRPCRequest represents a JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response
type JSONRPCResponse struct {
	ID      interface{}   `json:"id"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
	JSONRPC string        `json:"jsonrpc"`
}

// JSONRPCError represents a JSON-RPC 2.0 error
type JSONRPCError struct {
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message"`
	Code    int         `json:"code"`
}

// SSEEvent represents a Server-Sent Event
type SSEEvent struct {
	Data  interface{}
	ID    string
	Event string
}

// MCPSession represents an MCP session
//
//nolint:govet
type MCPSession struct {
	CreatedAt  time.Time
	LastActive time.Time
	ID         string
	Protocol   string
	Ctx        context.Context
	Notifier   chan SSEEvent
	cancel     context.CancelFunc
}

// SessionManager manages MCP sessions
type SessionManager struct {
	sessions map[string]*MCPSession
	mu       sync.RWMutex
}

// NewSessionManager creates a new session manager
func NewSessionManager() *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*MCPSession),
	}
	go sm.cleanupLoop()
	return sm
}

func (sm *SessionManager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		sm.cleanup()
	}
}

func (sm *SessionManager) cleanup() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	cutoff := time.Now().Add(-30 * time.Minute)
	for id, session := range sm.sessions {
		if session.LastActive.Before(cutoff) {
			sm.deleteSessionUnsafe(id)
		}
	}
}

// CreateSession creates a new session
func (sm *SessionManager) CreateSession(protocol string) *MCPSession {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	session := &MCPSession{
		ID:         generateSessionID(),
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
		Protocol:   protocol,
		Notifier:   make(chan SSEEvent, 10),
		Ctx:        ctx,
		cancel:     cancel,
	}
	sm.sessions[session.ID] = session
	return session
}

// GetSession retrieves a session by ID
func (sm *SessionManager) GetSession(id string) *MCPSession {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	session, ok := sm.sessions[id]
	if !ok {
		return nil
	}
	session.LastActive = time.Now()
	return session
}

// DeleteSession removes a session
func (sm *SessionManager) DeleteSession(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.deleteSessionUnsafe(id)
}

func (sm *SessionManager) deleteSessionUnsafe(id string) {
	if session, ok := sm.sessions[id]; ok {
		session.cancel()
		close(session.Notifier)
		delete(sm.sessions, id)
	}
}

func generateSessionID() string {
	return fmt.Sprintf("mcp_%d_%d", time.Now().UnixNano(), time.Now().UnixNano()%10000)
}

// StreamableHTTPHandler handles Streamable HTTP requests
type StreamableHTTPHandler struct {
	sessions     *SessionManager
	proxyHandler func(ctx context.Context, req Request, w http.ResponseWriter, r *http.Request) error
}

// NewStreamableHTTPHandler creates a new handler
func NewStreamableHTTPHandler(proxyHandler func(ctx context.Context, req Request, w http.ResponseWriter, r *http.Request) error) *StreamableHTTPHandler {
	return &StreamableHTTPHandler{
		sessions:     NewSessionManager(),
		proxyHandler: proxyHandler,
	}
}

// Handle processes Streamable HTTP requests
func (h *StreamableHTTPHandler) Handle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method == http.MethodGet {
		// This is the persistent connection for receiving SSE events
		h.handleNewSSESession(w, r)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONRPCError(w, nil, -32700, "Parse error", nil)
		return
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONRPCError(w, nil, -32700, "Parse error", nil)
		return
	}

	if req.JSONRPC != "2.0" {
		writeJSONRPCError(w, req.ID, -32600, "Invalid Request", "jsonrpc must be '2.0'")
		return
	}

	// Get or create session
	sessionID := r.Header.Get(HeaderMCPSessionID)
	var session *MCPSession
	if sessionID != "" {
		session = h.sessions.GetSession(sessionID)
	}

	// Allow POST requests without session for initialize method
	// This supports clients that don't maintain a persistent SSE connection
	if session == nil {
		if req.Method == "initialize" {
			session = h.sessions.CreateSession(r.Header.Get(HeaderMCPProtocolVer))
			logger.Info().Str("session", session.ID).Msg("Session created via POST initialize")
		} else {
			writeJSONRPCError(w, req.ID, -32603, "Invalid Request", "Session not found. Please establish a session with a GET request first.")
			return
		}
	}

	// Set session ID in response
	w.Header().Set(HeaderMCPSessionID, session.ID)

	logger.Debug().
		Str("method", req.Method).
		Str("session", session.ID).
		Msg("Streamable HTTP request")

	switch req.Method {
	case "initialize":
		h.handleInitialize(ctx, w, req, session)
	case "notifications/initialized":
		h.handleNotification(ctx, w, req, session)
	default:
		h.routeMethod(ctx, w, req, session, r)
	}
}

func (h *StreamableHTTPHandler) handleNewSSESession(w http.ResponseWriter, r *http.Request) {
	// Create a new session
	session := h.sessions.CreateSession(r.Header.Get(HeaderMCPProtocolVer))
	logger.Info().Str("session", session.ID).Msg("New SSE session established")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Expose-Headers", HeaderMCPSessionID)
	w.Header().Set(HeaderMCPSessionID, session.ID) // Send session ID back to client

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		h.sessions.DeleteSession(session.ID)
		return
	}

	// Send a welcome/init event
	h.writeSSEEvent(w, flusher, SSEEvent{
		Event: "session_created",
		Data:  map[string]interface{}{"session_id": session.ID},
	})

	// Listen for events on the session's notifier channel and write them to the response
	for {
		select {
		case <-r.Context().Done():
			// Client disconnected
			logger.Info().Str("session", session.ID).Msg("SSE session disconnected")
			h.sessions.DeleteSession(session.ID)
			return
		case event, ok := <-session.Notifier:
			if !ok {
				// Channel closed by the server
				logger.Info().Str("session", session.ID).Msg("SSE session channel closed")
				return
			}
			h.writeSSEEvent(w, flusher, event)
		}
	}
}

func (h *StreamableHTTPHandler) routeMethod(ctx context.Context, w http.ResponseWriter, req JSONRPCRequest, session *MCPSession, r *http.Request) {
	switch req.Method {
	case "ping":
		h.handlePing(ctx, w, req, session)
	case "resources/list":
		h.handleResourcesList(ctx, w, req, session)
	case "resources/read":
		h.handleResourcesRead(ctx, w, req, session)
	case "tools/list":
		h.handleToolsList(ctx, w, req, session)
	case "tools/call":
		h.handleToolsCall(ctx, w, req, session, r)
	case "models/list":
		h.handleModelsList(ctx, w, req, session)
	default:
		writeJSONRPCError(w, req.ID, -32601, "Method not found", nil)
	}
}

func (h *StreamableHTTPHandler) handlePing(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, _ *MCPSession) {
	writeJSONRPCResponse(w, req.ID, map[string]interface{}{})
}

func (h *StreamableHTTPHandler) handleInitialize(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, _ *MCPSession) {
	result := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{},
			"resources": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "AI Revolver",
			"version": "1.0.0",
		},
	}
	writeJSONRPCResponse(w, req.ID, result)
}

func (h *StreamableHTTPHandler) handleNotification(_ context.Context, w http.ResponseWriter, _ JSONRPCRequest, _ *MCPSession) {
	// Notifications don't require a response
	w.WriteHeader(http.StatusAccepted)
}

func (h *StreamableHTTPHandler) handleResourcesList(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, _ *MCPSession) {
	resources := []map[string]interface{}{
		{
			"uri":         "ai-revolver://config",
			"name":        "Current Configuration",
			"description": "The current (masked) configuration of the AI Revolver proxy",
			"mimeType":    "application/json",
		},
		{
			"uri":         "ai-revolver://stats",
			"name":        "Proxy Statistics",
			"description": "Real-time performance and usage statistics for all providers",
			"mimeType":    "application/json",
		},
		{
			"uri":         "ai-revolver://logs",
			"name":        "System Logs",
			"description": "Recent system logs from the in-memory ring buffer. Use 'level' query param to filter.",
			"mimeType":    "application/json",
		},
	}

	result := map[string]interface{}{
		"resources": resources,
	}
	writeJSONRPCResponse(w, req.ID, result)
}

func (h *StreamableHTTPHandler) handleResourcesRead(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, _ *MCPSession) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", nil)
		return
	}

	uri := params.URI
	var content string
	var err error

	switch {
	case uri == "ai-revolver://config":
		cfg := config.GetMaskedConfig()
		data, _ := json.MarshalIndent(cfg, "", "  ")
		content = string(data)
	case uri == "ai-revolver://stats":
		stats := GlobalMetricsStore.GetStats()
		data, _ := json.MarshalIndent(stats, "", "  ")
		content = string(data)
	case strings.HasPrefix(uri, "ai-revolver://logs"):
		level := zerolog.NoLevel
		if strings.Contains(uri, "level=") {
			parts := strings.Split(uri, "level=")
			if len(parts) > 1 {
				levelStr := strings.Split(parts[1], "&")[0]
				l, parseErr := zerolog.ParseLevel(levelStr)
				if parseErr == nil {
					level = l
				}
			}
		}
		logs := logger.GlobalRingBuffer.Get(level)
		data, _ := json.MarshalIndent(logs, "", "  ")
		content = string(data)
	default:
		writeJSONRPCError(w, req.ID, -32602, "Unknown resource URI", uri)
		return
	}

	if err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", err.Error())
		return
	}

	result := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"uri":      uri,
				"mimeType": "application/json",
				"text":     content,
			},
		},
	}
	writeJSONRPCResponse(w, req.ID, result)
}

func (h *StreamableHTTPHandler) handleToolsList(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, _ *MCPSession) {
	tools := []map[string]interface{}{
		{
			"name":        "chat_completion",
			"description": "Send a chat completion request to an AI model",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Model name (e.g., 'gpt-4', 'claude-3-opus')",
					},
					"messages": map[string]interface{}{
						"type":        "array",
						"description": "Array of messages",
					},
					"stream": map[string]interface{}{
						"type":        "boolean",
						"description": "Enable streaming response",
						"default":     false,
					},
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Specific provider to use (optional)",
					},
				},
				"required": []string{"messages"},
			},
		},
		{
			"name":        "update_config",
			"description": "Apply a JSON Patch to the server configuration. User confirmation is required before calling this.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"patch": map[string]interface{}{
						"type":        "array",
						"description": "JSON Patch array (e.g., [{\"op\": \"replace\", \"path\": \"/providers/0/priority\", \"value\": 10}])",
					},
				},
				"required": []string{"patch"},
			},
		},
		{
			"name":        "test_provider",
			"description": "Test connectivity to a specific provider by performing a minimal health check",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Name of the provider to test",
					},
				},
				"required": []string{"provider"},
			},
		},
		{
			"name":        "analyze_failure",
			"description": "Request analysis of a provider failure. Call this when you receive a sampling request or notice persistent errors.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"error_text": map[string]interface{}{
						"type":        "string",
						"description": "The error text to analyze",
					},
					"session_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional MCP session ID",
					},
				},
				"required": []string{"error_text"},
			},
		},
		{
			"name":        "read_config",
			"description": "Read the current (masked) configuration of the AI Revolver proxy",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "read_stats",
			"description": "Read real-time performance and usage statistics for all providers",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Filter stats by provider name (optional)",
					},
				},
			},
		},
		{
			"name":        "read_logs",
			"description": "Read recent system logs from the in-memory ring buffer",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"level": map[string]interface{}{
						"type":        "string",
						"description": "Log level filter (debug, info, warn, error)",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of log entries to return (default: all)",
					},
				},
			},
		},
		{
			"name":        "get_active_model",
			"description": "Get the last successfully used model and provider",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "get_blocked_models",
			"description": "Get the list of models currently blocked due to failures or high latency",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "get_circuit_breaker_settings",
			"description": "Get current latency threshold and block duration for the circuit breaker",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "set_latency_threshold",
			"description": "Set the latency threshold (ms) for blocking slow models",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"threshold_ms": map[string]interface{}{
						"type":        "integer",
						"description": "Latency threshold in milliseconds",
					},
				},
				"required": []string{"threshold_ms"},
			},
		},
		{
			"name":        "set_block_duration",
			"description": "Set the duration for which a model is blocked after failure or high latency",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"duration_sec": map[string]interface{}{
						"type":        "integer",
						"description": "Block duration in seconds",
					},
				},
				"required": []string{"duration_sec"},
			},
		},
		{
			"name":        "reset_failures",
			"description": "Reset all failure counters and unblock all models",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "clear_last_model",
			"description": "Clear the record of the last successful model",
			"inputSchema": map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			"name":        "scout_provider",
			"description": "Fetch available models directly from a provider's API",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"provider": map[string]interface{}{
						"type":        "string",
						"description": "Provider name",
					},
				},
				"required": []string{"provider"},
			},
		},
		{
			"name":        "add_provider",
			"description": "Add a new AI provider to the configuration",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":     map[string]interface{}{"type": "string"},
					"api_key":  map[string]interface{}{"type": "string"},
					"base_url": map[string]interface{}{"type": "string"},
					"priority": map[string]interface{}{"type": "integer", "default": 1},
				},
				"required": []string{"name", "base_url"},
			},
		},
		{
			"name":        "remove_provider",
			"description": "Remove a provider from the configuration",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "update_provider",
			"description": "Update provider settings (enabled, api_key, base_url, rate_limit, priority)",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":     map[string]interface{}{"type": "string"},
					"enabled":  map[string]interface{}{"type": "boolean"},
					"api_key":  map[string]interface{}{"type": "string"},
					"base_url": map[string]interface{}{"type": "string"},
					"priority": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "add_model",
			"description": "Add a new model to a provider",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"provider":   map[string]interface{}{"type": "string"},
					"name":       map[string]interface{}{"type": "string"},
					"max_tokens": map[string]interface{}{"type": "integer", "default": 4096},
				},
				"required": []string{"provider", "name"},
			},
		},
		{
			"name":        "remove_model",
			"description": "Remove a model from a provider",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"provider": map[string]interface{}{"type": "string"},
					"model":    map[string]interface{}{"type": "string"},
				},
				"required": []string{"provider", "model"},
			},
		},
		{
			"name":        "update_model",
			"description": "Update model capabilities and settings",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"provider":   map[string]interface{}{"type": "string"},
					"model":      map[string]interface{}{"type": "string"},
					"name":       map[string]interface{}{"type": "string", "description": "New name for the model"},
					"max_tokens": map[string]interface{}{"type": "integer"},
					"thinking":   map[string]interface{}{"type": "boolean"},
					"reasoning":  map[string]interface{}{"type": "boolean"},
					"tools":      map[string]interface{}{"type": "boolean"},
				},
				"required": []string{"provider", "model"},
			},
		},
		{
			"name":        "probe_model",
			"description": "Run a tool-use probe on a specific model to verify capabilities",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"provider": map[string]interface{}{"type": "string"},
					"model":    map[string]interface{}{"type": "string"},
				},
				"required": []string{"provider", "model"},
			},
		},
		{
			"name":        "probe_all_models",
			"description": "Run probes on all models of a provider",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"provider": map[string]interface{}{"type": "string"},
				},
				"required": []string{"provider"},
			},
		},
		{
			"name":        "optimize_provider",
			"description": "Sort models of a provider by their measured latency",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"provider": map[string]interface{}{"type": "string"},
				},
				"required": []string{"provider"},
			},
		},
	}

	result := map[string]interface{}{
		"tools": tools,
	}
	writeJSONRPCResponse(w, req.ID, result)
}

//nolint:gocyclo
func (h *StreamableHTTPHandler) handleToolsCall(ctx context.Context, w http.ResponseWriter, req JSONRPCRequest, _ *MCPSession, r *http.Request) {
	var params struct {
		Arguments map[string]interface{} `json:"arguments"`
		Name      string                 `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", nil)
		return
	}

	switch params.Name {
	case "chat_completion":
		h.handleChatCompletionTool(ctx, w, req, params.Arguments, r)
	case "update_config":
		h.handleUpdateConfigTool(ctx, w, req, params.Arguments)
	case "test_provider":
		h.handleTestProviderTool(ctx, w, req, params.Arguments)
	case "analyze_failure":
		h.handleAnalyzeFailureTool(ctx, w, req, params.Arguments)
	case "read_config":
		h.handleReadConfigTool(ctx, w, req)
	case "read_stats":
		h.handleReadStatsTool(ctx, w, req, params.Arguments)
	case "read_logs":
		h.handleReadLogsTool(ctx, w, req, params.Arguments)
	case "get_active_model":
		h.handleGetActiveModelTool(ctx, w, req)
	case "get_blocked_models":
		h.handleGetBlockedModelsTool(ctx, w, req)
	case "get_circuit_breaker_settings":
		h.handleGetCircuitBreakerSettingsTool(ctx, w, req)
	case "set_latency_threshold":
		h.handleSetLatencyThresholdTool(ctx, w, req, params.Arguments)
	case "set_block_duration":
		h.handleSetBlockDurationTool(ctx, w, req, params.Arguments)
	case "reset_failures":
		h.handleResetFailuresTool(ctx, w, req)
	case "clear_last_model":
		h.handleClearLastModelTool(ctx, w, req)
	case "scout_provider":
		h.handleScoutProviderTool(ctx, w, req, params.Arguments)
	case "add_provider":
		h.handleAddProviderTool(ctx, w, req, params.Arguments)
	case "remove_provider":
		h.handleRemoveProviderTool(ctx, w, req, params.Arguments)
	case "update_provider":
		h.handleUpdateProviderTool(ctx, w, req, params.Arguments)
	case "add_model":
		h.handleAddModelTool(ctx, w, req, params.Arguments)
	case "remove_model":
		h.handleRemoveModelTool(ctx, w, req, params.Arguments)
	case "update_model":
		h.handleUpdateModelTool(ctx, w, req, params.Arguments)
	case "probe_model":
		h.handleProbeModelTool(ctx, w, req, params.Arguments)
	case "probe_all_models":
		h.handleProbeAllModelsTool(ctx, w, req, params.Arguments)
	case "optimize_provider":
		h.handleOptimizeProviderTool(ctx, w, req, params.Arguments)
	default:
		writeJSONRPCError(w, req.ID, -32601, "Tool not found", fmt.Sprintf("Tool '%s' not found", params.Name))
	}
}

func (h *StreamableHTTPHandler) handleAnalyzeFailureTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	errorText := getStringParam(args, "error_text")
	if errorText == "" {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "error_text is required")
		return
	}

	logger.Info().
		Str("error_text", errorText).
		Str("session_id", getStringParam(args, "session_id")).
		Msg("Agent requested failure analysis")

	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": "Analysis request received. Please analyze the system state using 'ai-revolver://logs' and 'ai-revolver://config' resources to identify the root cause.",
			},
		},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleReadConfigTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest) {
	cfg := config.GetMaskedConfig()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", err.Error())
		return
	}
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

func (h *StreamableHTTPHandler) handleReadStatsTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	stats := GlobalMetricsStore.GetStats()

	// Optional provider filter
	if providerName, ok := args["provider"].(string); ok && providerName != "" {
		if providerStats, exists := stats[providerName]; exists {
			filteredStats := make(map[string]ProviderMetrics)
			filteredStats[providerName] = providerStats
			stats = filteredStats
		} else {
			// Provider not found, return empty but don't error
			stats = make(map[string]ProviderMetrics)
		}
	}

	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", err.Error())
		return
	}
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

func (h *StreamableHTTPHandler) handleReadLogsTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	level := zerolog.NoLevel
	if lvlStr, ok := args["level"].(string); ok && lvlStr != "" {
		if lvl, err := zerolog.ParseLevel(lvlStr); err == nil {
			level = lvl
		}
	}

	limit := 0
	if limitVal, ok := args["limit"].(float64); ok && limitVal > 0 {
		limit = int(limitVal)
	}

	logs := logger.GlobalRingBuffer.Get(level)

	// Apply limit if specified (return last N entries)
	if limit > 0 && limit < len(logs) {
		logs = logs[len(logs)-limit:]
	}

	data, err := json.MarshalIndent(logs, "", "  ")
	if err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", err.Error())
		return
	}
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

func (h *StreamableHTTPHandler) handleChatCompletionTool(ctx context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}, r *http.Request) {
	// Build proxy request from tool arguments
	proxyReq := Request{
		Provider: getStringParam(args, "provider"),
	}

	if model, ok := args["model"].(string); ok {
		proxyReq.Model = model
	} else {
		proxyReq.Model = "auto"
	}

	if messages, ok := args["messages"].([]interface{}); ok {
		proxyReq.Messages = convertMessages(messages)
	}

	if stream, ok := args["stream"].(bool); ok {
		proxyReq.Stream = stream
	}

	// Handle streaming vs non-streaming
	acceptHeader := r.Header.Get("Accept")
	isStreaming := proxyReq.Stream || strings.Contains(acceptHeader, ContentTypeSSE)

	if isStreaming {
		w.Header().Set("Content-Type", ContentTypeSSE)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSONRPCError(w, req.ID, -32603, "Internal error", "Streaming not supported")
			return
		}

		// Write initial response as SSE
		h.writeSSEEvent(w, flusher, SSEEvent{
			Event: "message",
			Data: map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"content": []map[string]interface{}{},
					"isError": false,
				},
			},
		})

		// Execute proxy request
		err := h.proxyHandler(ctx, proxyReq, w, r)
		if err != nil {
			h.writeSSEEvent(w, flusher, SSEEvent{
				Event: "error",
				Data: map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"error": map[string]interface{}{
						"code":    -32603,
						"message": err.Error(),
					},
				},
			})
		}

		// End event
		h.writeSSEEvent(w, flusher, SSEEvent{
			Event: "end",
			Data:  map[string]interface{}{},
		})
	} else {
		// Non-streaming: collect response and return as JSON
		proxyReq.Stream = false
		result, _, err := Proxy(ctx, proxyReq)
		if err != nil {
			writeJSONRPCError(w, req.ID, -32603, "Internal error", err.Error())
			return
		}

		// Convert to MCP tool result format
		content := []map[string]interface{}{}
		for _, choice := range result.Choices {
			msg := choice.Message

			// Handle tool calls if present
			if msg.ToolCalls != nil {
				var toolCalls []interface{}
				if err := json.Unmarshal(msg.ToolCalls, &toolCalls); err == nil {
					for _, tc := range toolCalls {
						if toolCallMap, ok := tc.(map[string]interface{}); ok {
							content = append(content, map[string]interface{}{
								"type":      "tool_call",
								"tool_call": toolCallMap,
							})
						}
					}
				}
			} else {
				// Handle text content
				content = append(content, map[string]interface{}{
					"type": "text",
					"text": msg.GetContentString(),
				})
			}
		}

		writeJSONRPCResponse(w, req.ID, map[string]interface{}{
			"content": content,
			"isError": false,
		})
	}
}

func (h *StreamableHTTPHandler) handleUpdateConfigTool(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	patch, ok := args["patch"].([]interface{})
	if !ok {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "patch must be an array")
		return
	}
	patchData, err := json.Marshal(patch)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", err.Error())
		return
	}
	if err := config.UpdateConfig(patchData); err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", err.Error())
		return
	}
	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": "Configuration updated successfully and saved to disk.",
			},
		},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleTestProviderTool(ctx context.Context, w http.ResponseWriter, req JSONRPCRequest, args map[string]interface{}) {
	providerName, ok := args["provider"].(string)
	if !ok {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", "provider must be a string")
		return
	}

	// Find provider
	cfg := config.GetConfig()
	var targetProvider *config.Provider
	for _, p := range cfg.Providers {
		if p.Name == providerName {
			targetProvider = &p
			break
		}
	}

	if targetProvider == nil {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params", fmt.Sprintf("Provider '%s' not found", providerName))
		return
	}

	if len(targetProvider.Models) == 0 {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", "Provider has no models")
		return
	}

	// Use first model for ping
	testModel := targetProvider.Models[0].Name
	testReq := Request{
		Model: testModel,
		Messages: []Message{
			{Role: "user", Content: "ping"},
		},
		MaxTokens: 1,
	}

	_, _, err := forwardRequest(ctx, *targetProvider, testModel, testReq)
	if err != nil {
		writeJSONRPCResponse(w, req.ID, map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("Provider test failed: %v", err),
				},
			},
			"isError": true,
		})
		return
	}

	writeJSONRPCResponse(w, req.ID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("Provider '%s' is reachable and responded successfully.", providerName),
			},
		},
		"isError": false,
	})
}

func (h *StreamableHTTPHandler) handleModelsList(_ context.Context, w http.ResponseWriter, req JSONRPCRequest, _ *MCPSession) {
	cfg := config.GetConfig()

	var models []map[string]interface{}
	for _, provider := range cfg.Providers {
		if !provider.IsEnabled() {
			continue
		}
		for _, model := range provider.Models {
			modelInfo := map[string]interface{}{
				"id":         model.Name,
				"name":       model.Name,
				"provider":   provider.Name,
				"max_tokens": model.MaxTokens,
				"capabilities": map[string]interface{}{
					"thinking":  model.Thinking,
					"reasoning": model.Reasoning,
					"tools":     model.Tools,
					"streaming": true,
				},
			}
			models = append(models, modelInfo)
		}
	}

	result := map[string]interface{}{
		"models": models,
		"count":  len(models),
	}
	writeJSONRPCResponse(w, req.ID, result)
}

func (h *StreamableHTTPHandler) writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event SSEEvent) {
	if event.ID != "" {
		_, _ = fmt.Fprintf(w, "id: %s\n", event.ID)
	}
	if event.Event != "" {
		_, _ = fmt.Fprintf(w, "event: %s\n", event.Event)
	}
	data, _ := json.Marshal(event.Data)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		_, _ = fmt.Fprintf(w, "data: %s\n", line)
	}
	_, _ = fmt.Fprint(w, "\n")
	flusher.Flush()
}

func writeJSONRPCResponse(w http.ResponseWriter, id interface{}, result interface{}) {
	w.Header().Set("Content-Type", ContentTypeJSON)
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func writeJSONRPCError(w http.ResponseWriter, id interface{}, code int, message string, data interface{}) {
	w.Header().Set("Content-Type", ContentTypeJSON)
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func getStringParam(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func convertMessages(messages []interface{}) []Message {
	var result []Message
	for _, m := range messages {
		if msgMap, ok := m.(map[string]interface{}); ok {
			msg := Message{
				Role:    getStringParam(msgMap, "role"),
				Content: msgMap["content"],
			}

			// Handle tool calls if present
			if toolCalls, ok := msgMap["tool_calls"]; ok {
				if toolCallsJSON, err := json.Marshal(toolCalls); err == nil {
					msg.ToolCalls = json.RawMessage(toolCallsJSON)
				}
			}

			// Handle tool call ID if present
			if toolCallID, ok := msgMap["tool_call_id"]; ok {
				if str, ok := toolCallID.(string); ok {
					msg.ToolCallID = str
				}
			}

			result = append(result, msg)
		}
	}
	return result
}

var (
	mcpHandler     *StreamableHTTPHandler
	mcpHandlerOnce sync.Once

	samplingCooldown = 5 * time.Minute
	lastSamplingTime = make(map[string]time.Time)
	samplingMu       sync.Mutex
)

func init() {
	OnPersistentFailure = func(provider string, lastError error) {
		samplingMu.Lock()
		defer samplingMu.Unlock()

		lastTime, ok := lastSamplingTime[provider]
		if ok && time.Since(lastTime) < samplingCooldown {
			return
		}

		lastSamplingTime[provider] = time.Now()

		if mcpHandler != nil {
			mcpHandler.broadcastSamplingRequest(provider, lastError)
		}
	}
}

// broadcastSamplingRequest sends a sampling/createMessage request to all active sessions
func (h *StreamableHTTPHandler) broadcastSamplingRequest(provider string, lastError error) {
	h.sessions.mu.RLock()
	sessions := make([]*MCPSession, 0, len(h.sessions.sessions))
	for _, s := range h.sessions.sessions {
		sessions = append(sessions, s)
	}
	h.sessions.mu.RUnlock()

	errorMsg := "nil"
	if lastError != nil {
		errorMsg = lastError.Error()
	}

	msg := fmt.Sprintf("Provider %s has failed 3 times consecutively. Last error: %s. Please analyze the system state using 'ai-revolver://logs' and 'ai-revolver://config' resources and suggest a fix or switch provider.", provider, errorMsg)

	params := map[string]interface{}{
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": map[string]interface{}{
					"type": "text",
					"text": msg,
				},
			},
		},
		"maxTokens": 1024,
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      fmt.Sprintf("sampling_%d", time.Now().UnixNano()),
		Method:  "sampling/createMessage",
		Params:  marshalParams(params),
	}

	logger.Info().
		Str("provider", provider).
		Int("sessions", len(sessions)).
		Msg("Broadcasting sampling request to active sessions")

	for _, session := range sessions {
		select {
		case <-session.Ctx.Done():
			// Don't try to send to a closed session
			continue
		case session.Notifier <- SSEEvent{Event: "message", Data: req, ID: req.ID.(string)}:
			logger.Debug().
				Str("session", session.ID).
				Str("method", req.Method).
				Msg("Sent sampling request to session")
		default:
			logger.Warn().
				Str("session", session.ID).
				Msg("Failed to send sampling request: notifier channel full or closed")
		}
	}
}

func marshalParams(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return json.RawMessage(data)
}

// HandleMCPEndpoint is the main handler for /mcp endpoint
func HandleMCPEndpoint(w http.ResponseWriter, r *http.Request) {
	mcpHandlerOnce.Do(func() {
		mcpHandler = NewStreamableHTTPHandler(handleStreamProxyRequest)
	})
	mcpHandler.Handle(w, r)
}

// handleStreamProxyRequest handles streaming proxy requests for MCP
func handleStreamProxyRequest(ctx context.Context, req Request, w http.ResponseWriter, _ *http.Request) error {
	// Get provider and model
	provider, model, err := getProviderAndModel(ctx, req)
	if err != nil {
		return err
	}

	// Forward streaming request
	_, err = forwardStreamRequest(ctx, *provider, model, req, w)
	return err
}

