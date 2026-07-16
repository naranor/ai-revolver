// Package proxy implements the core logic for intelligent model selection and request forwarding
package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"ai-proxy/config"
	"ai-proxy/db"
	"ai-proxy/logger"
)

// Configurable settings
var (
	OllamaStreamBufferSize int64 = 2 * 1024 * 1024 // Default 2MB
	OpenAIStreamBufferSize int64 = 2 * 1024 * 1024 // Default 2MB
	TraceMode              bool                    // Log all payloads and responses
)

// Pool for strings.Builder to reduce allocations in GetContentString
var builderPool = sync.Pool{
	New: func() interface{} {
		return new(strings.Builder)
	},
}

// tryFallbackStreamModel tries the fallback model with the lowest EWMA latency when stream candidates are blocked.
// Unlike tryFallbackModel, it writes the streaming response directly to the provided ResponseWriter.
func tryFallbackStreamModel(ctx context.Context, cfg config.Config, req Request, w http.ResponseWriter) error {
	fallbackProvider, fallbackModel, fallbackLatency := GetBestFallbackModel()
	if fallbackProvider == "" || fallbackModel == "" {
		logger.Warn().Msg("All stream models blocked, no fallback available")
		return fmt.Errorf("no fallback available")
	}

	logger.Warn().
		Str("provider", fallbackProvider).
		Str("model", fallbackModel).
		Int64("latency_ms", fallbackLatency).
		Msg("All stream models blocked, using fallback")
	db.LogError("proxy", "fallback_stream",
		fmt.Sprintf("All stream models blocked, using fallback %s/%s with latency %dms",
			fallbackProvider, fallbackModel, fallbackLatency))

	for _, p := range cfg.Providers {
		if p.Name != fallbackProvider {
			continue
		}

		startTime := time.Now()
		code, err := forwardStreamRequest(ctx, p, fallbackModel, req, w)
		latency := time.Since(startTime).Milliseconds()

		if err == nil {
			RecordSuccess(p.Name, fallbackModel, latency)
			onSuccess(ProviderModelPair{Provider: p, Model: fallbackModel}, latency, 1, latency, req)
			return nil
		}

		onFailure(ProviderModelPair{Provider: p, Model: fallbackModel}, latency, 1, code, err, req)
		return err
	}

	return fmt.Errorf("fallback provider %s not found in configuration", fallbackProvider)
}

// Request represents an incoming chat completion request
type Request struct {
	ExtraParams map[string]interface{} `json:"extra_params,omitempty"`
	Model       string                 `json:"model"`
	Provider    string                 `json:"provider,omitempty"`
	Messages    []Message              `json:"messages"`
	Tools       []interface{}          `json:"tools,omitempty"`
	ToolChoice  interface{}            `json:"tool_choice,omitempty"`
	RawBody     []byte                 `json:"-"`
	MaxTokens   int                    `json:"max_tokens,omitempty"`
	Temperature float64                `json:"temperature,omitempty"`
	TopP        float64                `json:"top_p,omitempty"`
	Stream      bool                   `json:"stream,omitempty"`
	Probe       bool                   `json:"probe,omitempty"`
	IsWarmup    bool                   `json:"is_warmup,omitempty"`
}

// RequestPool is a pool for Request to reduce allocations
var RequestPool = sync.Pool{
	New: func() interface{} {
		return &Request{}
	},
}

// KnownRequestFields are explicitly handled by this proxy
var KnownRequestFields = map[string]bool{
	"model":        true,
	"messages":     true,
	"max_tokens":   true,
	"temperature":  true,
	"top_p":        true,
	"stream":       true,
	"extra_params": true,
	"provider":     true,
	"probe":        true,
	"tools":        true,
	"tool_choice":  true,
	"is_warmup":    true,
}

// Message represents a single chat message
type Message struct {
	Content    interface{}     `json:"content"`
	Role       string          `json:"role"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
}

// GetContentString converts Message content to string
func (m *Message) GetContentString() string {
	if m.Content == nil {
		return ""
	}
	switch v := m.Content.(type) {
	case string:
		return v
	case []interface{}:
		b := builderPool.Get().(*strings.Builder)
		b.Reset()
		for _, block := range v {
			if blockMap, ok := block.(map[string]interface{}); ok {
				if text, ok := blockMap["text"].(string); ok {
					b.WriteString(text)
				}
			}
		}
		result := b.String()
		builderPool.Put(b)
		return result
	default:
		return fmt.Sprintf("%v", v)
	}
}

// Usage represents token usage statistics
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Response represents a standard chat completion response
type Response struct {
	ID      string   `json:"id,omitempty"`
	Object  string   `json:"object,omitempty"`
	Created int64    `json:"created,omitempty"`
	Model   string   `json:"model,omitempty"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

// Choice represents a single completion choice
type Choice struct {
	Index        int      `json:"index"`
	Message      Message  `json:"message,omitempty"`
	Delta        *Message `json:"delta,omitempty"`
	FinishReason string   `json:"finish_reason,omitempty"`
}

// OllamaMessage represents a message in Ollama API
type OllamaMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls []any  `json:"tool_calls,omitempty"`
}

// OllamaResponse represents a response from Ollama API (/api/chat)
type OllamaResponse struct {
	Model           string        `json:"model"`
	CreatedAt       string        `json:"created_at"`
	Message         OllamaMessage `json:"message"`
	Done            bool          `json:"done"`
	TotalDuration   int64         `json:"total_duration"`
	LoadDuration    int64         `json:"load_duration"`
	PromptEvalCount int           `json:"prompt_eval_count"`
	EvalCount       int           `json:"eval_count"`
}

// ProviderModelPair represents a provider:model combination
type ProviderModelPair struct {
	Model    string
	Provider config.Provider
}

// ProxyFunc is a variable allowing Proxy to be mocked in tests
var ProxyFunc = Proxy

// Proxy handles a chat completion request with automatic failover
func Proxy(ctx context.Context, req Request) (*Response, int, error) {

	if TraceMode {
		logger.Info().
			Str("model", req.Model).
			Str("provider", req.Provider).
			Str("payload", string(req.RawBody)).
			Msg("TRACE: Client Request")
	}

	cfg := config.GetConfig()
	candidates := getCandidates(cfg, req.Model, req.Provider)

	if len(candidates) == 0 {
		return nil, 0, fmt.Errorf("no providers available")
	}

	resp, code, err := tryProviders(ctx, cfg, candidates, req)
	if TraceMode && err == nil && resp != nil {
		respJSON, _ := json.Marshal(resp)
		logger.Info().
			Str("response", string(respJSON)).
			Msg("TRACE: Client Response")
	}
	return resp, code, err
}

// getCandidates returns filtered candidates for the request
func getCandidates(cfg config.Config, model string, provider string) []ProviderModelPair {
	// If a specific provider is requested, bypass the 'enabled' check and tiered selection
	if provider != "" {
		for _, p := range cfg.Providers {
			if p.Name != provider {
				continue
			}

			if model != "" && model != "auto" {
				// Check if model exists in this provider
				for _, m := range p.Models {
					if m.Name == model {
						return []ProviderModelPair{{Provider: p, Model: m.Name}}
					}
				}
				return nil // Model not found in provider
			}

			// Provider found, return all its models
			var pairs []ProviderModelPair
			for _, m := range p.Models {
				pairs = append(pairs, ProviderModelPair{Provider: p, Model: m.Name})
			}
			return pairs
		}
		return nil // Provider not found
	}

	candidates := getAllCandidates(cfg)
	if model != "" && model != "auto" {
		candidates = filterByModel(candidates, model)
	}
	return candidates
}

// tryProviders tries each candidate until one succeeds
func tryProviders(ctx context.Context, cfg config.Config, candidates []ProviderModelPair, req Request) (*Response, int, error) {

	var lastErr error
	var lastCode int
	var totalLatency int64
	var attemptCount int
	var skippedCount int

	for _, candidate := range candidates {
		// Check if the overall request context was canceled
		if ctx.Err() == context.Canceled {
			return nil, 0, ctx.Err()
		}

		if shouldSkipProvider(cfg, candidate) {
			skippedCount++
			continue
		}

		if attemptCount >= cfg.MaxRetries {
			logger.Warn().
				Int("attempt_count", attemptCount).
				Int("max_retries", cfg.MaxRetries).
				Msg("Reached maximum retries, stopping failover")
			break
		}

		attemptCount++

		// Per-candidate timeout (15s) to ensure we switch to backup if provider hangs
		candidateCtx, cancel := context.WithTimeout(ctx, GetResponseTimeout())
		result, latency, code, err := forwardRequestWithLatency(candidateCtx, candidate.Provider, candidate.Model, req)
		cancel()

		// If the main context was canceled, return immediately
		if ctx.Err() == context.Canceled {
			return nil, 0, ctx.Err()
		}

		totalLatency += latency
		lastCode = code

		// Successful if no error and code is 200
		if err == nil && code == 200 {
			RecordSuccess(candidate.Provider.Name, candidate.Model, latency)
			onSuccess(candidate, latency, attemptCount, totalLatency, req)
			return result, code, nil
		}

		// Non-200 with no error is still a failure for failover purposes
		if err == nil && code != 200 {
			err = fmt.Errorf("upstream returned status %d", code)
		}

		// Log warning if it was a timeout
		if candidateCtx.Err() == context.DeadlineExceeded {
			logger.Warn().
				Str("provider", candidate.Provider.Name).
				Str("model", candidate.Model).
				Msg("Provider timed out (15s), trying next candidate")
		}

		onFailure(candidate, latency, attemptCount, code, err, req)
		lastErr = err

		if req.Probe {
			break
		}
	}

	if attemptCount == 0 && skippedCount > 0 {
		if result, err := tryFallbackModel(ctx, cfg, req); err == nil {
			return result, 200, nil
		}
	}

	return nil, lastCode, formatAllProvidersFailedError(attemptCount, lastErr)
}

// shouldSkipProvider checks if a provider should be skipped
func shouldSkipProvider(cfg config.Config, candidate ProviderModelPair) bool {
	status, _ := GetModelStatus(candidate.Provider.Name, candidate.Model)
	if status == StatusBlockedFatal {
		logger.Warn().
			Str("provider", candidate.Provider.Name).
			Str("model", candidate.Model).
			Msg("Skipping fatally blocked model")
		db.LogError(candidate.Provider.Name, "model_blocked_fatal",
			fmt.Sprintf("Skipping fatally blocked model %s/%s", candidate.Provider.Name, candidate.Model))
		return true
	}

	if isRateLimited(cfg, candidate.Provider) {
		logger.Warn().
			Str("provider", candidate.Provider.Name).
			Msg("Skipping rate-limited provider")
		db.LogError(candidate.Provider.Name, "rate_limited",
			fmt.Sprintf("Skipping rate-limited provider %s", candidate.Provider.Name))
		return true
	}

	return false
}

// onSuccess handles successful request completion
func onSuccess(candidate ProviderModelPair, latency int64, attemptCount int, _ int64, req Request) {
	if !req.IsWarmup {
		SetLastSuccessfulModel(candidate.Provider.Name, candidate.Model, latency)
	}

	// Log when model became active after failover
	if attemptCount > 1 {
		logger.Info().
			Str("provider", candidate.Provider.Name).
			Str("model", candidate.Model).
			Msg("Model became active")
	}

	// Always log successful requests to DB
	db.LogRequest(candidate.Provider.Name, candidate.Model, 200, int(latency), req.IsWarmup)
}

// onFailure handles failed request
func onFailure(candidate ProviderModelPair, latency int64, attemptCount int, code int, err error, req Request) {
	logger.Error().
		Str("provider", candidate.Provider.Name).
		Str("model", candidate.Model).
		Int("attempt", attemptCount).
		Int("http_code", code).
		Int64("latency_ms", latency).
		Err(err).
		Msg("Provider failed, switching to next")
	db.LogRequest(candidate.Provider.Name, candidate.Model, code, int(latency), req.IsWarmup)
	db.LogError(candidate.Provider.Name, "model_switch",
		fmt.Sprintf("Model %s/%s failed (code %d, attempt %d), switching to next model. Error: %v",
			candidate.Provider.Name, candidate.Model, code, attemptCount, err))
	RecordFailure(candidate.Provider.Name, candidate.Model, code, err)
}

// tryFallbackModel tries the fallback model with the lowest EWMA latency when all are blocked.
func tryFallbackModel(ctx context.Context, cfg config.Config, req Request) (*Response, error) {
	fallbackProvider, fallbackModel, fallbackLatency := GetBestFallbackModel()
	if fallbackProvider == "" || fallbackModel == "" {
		logger.Warn().Msg("All models blocked, no fallback available")
		return nil, fmt.Errorf("no fallback available")
	}

	logger.Warn().
		Str("provider", fallbackProvider).
		Str("model", fallbackModel).
		Int64("latency_ms", fallbackLatency).
		Msg("All models blocked, using fallback")
	db.LogError("proxy", "fallback",
		fmt.Sprintf("All models blocked, using fallback %s/%s with latency %dms",
			fallbackProvider, fallbackModel, fallbackLatency))

	for _, p := range cfg.Providers {
		if p.Name == fallbackProvider {
			candidateCtx, cancel := context.WithTimeout(ctx, GetResponseTimeout())
			result, latency, code, err := forwardRequestWithLatency(candidateCtx, p, fallbackModel, req)
			cancel()

			if err == nil {
				RecordSuccess(ProviderModelPair{Provider: p, Model: fallbackModel}.Provider.Name, fallbackModel, latency)
				onSuccess(ProviderModelPair{Provider: p, Model: fallbackModel}, latency, 1, latency, req)
				return result, nil
			}
			logger.Error().
				Str("provider", fallbackProvider).
				Str("model", fallbackModel).
				Err(err).
				Msg("Fallback also failed")
			RecordFailure(fallbackProvider, fallbackModel, code, err)
			return nil, err
		}
	}

	return nil, fmt.Errorf("fallback provider %s not found in configuration", fallbackProvider)
}

// formatAllProvidersFailedError creates an appropriate error message
func formatAllProvidersFailedError(attemptCount int, lastErr error) error {
	if lastErr != nil {
		logger.Error().
			Int("attempts", attemptCount).
			Err(lastErr).
			Msg("All providers failed")
		return fmt.Errorf("all providers failed after %d attempts, last error: %w", attemptCount, lastErr)
	}
	logger.Error().Msg("All providers failed, no attempts made")
	return fmt.Errorf("all providers failed")
}

// filterByModel filters candidates to only include the specified model
func filterByModel(candidates []ProviderModelPair, model string) []ProviderModelPair {
	var filtered []ProviderModelPair
	for _, c := range candidates {
		if c.Model == model {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// isRateLimited checks if a provider is currently rate limited
func isRateLimited(cfg config.Config, provider config.Provider) bool {
	for _, p := range cfg.Providers {
		if p.Name == provider.Name {
			if p.RateLimit > 0 && p.CurrentUsage >= p.RateLimit {
				return true
			}
			return false
		}
	}
	return false
}

// forwardRequestWithLatency forwards a request and returns the latency and status code
func forwardRequestWithLatency(ctx context.Context, provider config.Provider, model string, req Request) (*Response, int64, int, error) {
	startTime := time.Now()
	result, code, err := forwardRequest(ctx, provider, model, req)
	latency := time.Since(startTime).Milliseconds()
	GlobalMetricsStore.ReportRequest(provider.Name, latency, err)
	return result, latency, code, err
}

// trackingResponseWriter wraps http.ResponseWriter to track if any data was written
type trackingResponseWriter struct {
	http.ResponseWriter
	written bool
}

func (w *trackingResponseWriter) WriteHeader(code int) {
	w.written = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *trackingResponseWriter) Write(b []byte) (int, error) {
	if len(b) > 0 {
		w.written = true
	}
	return w.ResponseWriter.Write(b)
}

func (w *trackingResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *trackingResponseWriter) Written() bool {
	return w.written
}

// Stream handles streaming chat completion requests
func Stream(ctx context.Context, req Request, w http.ResponseWriter) error {

	if TraceMode {
		logger.Info().
			Str("model", req.Model).
			Str("payload", string(req.RawBody)).
			Msg("TRACE: Client Stream Request")
	}

	cfg := config.GetConfig()
	candidates := getCandidates(cfg, req.Model, req.Provider)

	if len(candidates) == 0 {
		return fmt.Errorf("no providers available")
	}

	req.Stream = true

	var lastErr error
	var totalLatency int64
	var attemptCount int
	var skippedCount int

	tw := &trackingResponseWriter{ResponseWriter: w}

	for _, candidate := range candidates {
		// Check if the overall request context was canceled
		if ctx.Err() == context.Canceled {
			return ctx.Err()
		}

		if shouldSkipProvider(cfg, candidate) {
			skippedCount++
			continue
		}

		if attemptCount >= cfg.MaxRetries {
			logger.Warn().
				Int("attempt_count", attemptCount).
				Int("max_retries", cfg.MaxRetries).
				Msg("Reached maximum retries for stream, stopping failover")
			break
		}

		attemptCount++
		startTime := time.Now()

		code, err := forwardStreamRequest(ctx, candidate.Provider, candidate.Model, req, tw)

		// If the main context was canceled, return immediately
		if ctx.Err() == context.Canceled {
			return ctx.Err()
		}

		latency := time.Since(startTime).Milliseconds()
		totalLatency += latency

		if err == nil {
			RecordSuccess(candidate.Provider.Name, candidate.Model, latency)
			SetLastSuccessfulModel(candidate.Provider.Name, candidate.Model, latency)

			// Log when model became active after failover
			if attemptCount > 1 {
				logger.Info().
					Str("provider", candidate.Provider.Name).
					Str("model", candidate.Model).
					Msg("Model became active (stream)")
			}

			logger.Debug().
				Str("provider", candidate.Provider.Name).
				Str("model", candidate.Model).
				Int64("latency_ms", latency).
				Msg("Stream completed successfully")

			db.LogRequest(candidate.Provider.Name, candidate.Model, 200, int(latency), req.IsWarmup)
			return nil
		}

		// If data was already written to the client, we CANNOT failover
		if tw.Written() {
			logger.Warn().
				Str("provider", candidate.Provider.Name).
				Str("model", candidate.Model).
				Err(err).
				Msg("Stream failed after data was written to client, cannot retry")
			onFailure(candidate, latency, attemptCount, code, err, req)
			return err
		}

		// Log warning if it was a timeout
		if errors.Is(err, context.DeadlineExceeded) {
			logger.Warn().
				Str("provider", candidate.Provider.Name).
				Str("model", candidate.Model).
				Msg("Stream attempt timed out, trying next candidate")
		}

		onFailure(candidate, latency, attemptCount, code, err, req)
		lastErr = err

		if req.Probe {
			break
		}
	}

	if attemptCount == 0 && skippedCount > 0 {
		if err := tryFallbackStreamModel(ctx, cfg, req, tw); err != nil {
			lastErr = err
		} else {
			return nil
		}
	}

	if attemptCount > 0 && lastErr == nil {
		lastErr = fmt.Errorf("all stream attempts exhausted without a successful response")
	}
	return formatAllProvidersFailedError(attemptCount, lastErr)
}

// forwardRequest forwards a request to a provider and returns status code
func forwardRequest(ctx context.Context, provider config.Provider, model string, req Request) (*Response, int, error) {
	endpoint := buildEndpoint(provider, "/chat/completions")
	body, err := buildRequestBody(provider, model, req)
	if err != nil {
		logger.Error().Err(err).Str("provider", provider.Name).Msg("Failed to build request body")
		return nil, 0, err
	}

	if TraceMode {
		logger.Info().
			Str("provider", provider.Name).
			Str("model", model).
			Str("endpoint", endpoint).
			Str("payload", string(body)).
			Msg("TRACE: Upstream Request")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		logger.Error().Err(err).Str("provider", provider.Name).Str("endpoint", endpoint).Msg("Failed to create HTTP request")
		return nil, 0, err
	}

	setRequestHeaders(httpReq, provider)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		logger.Error().
			Err(err).
			Str("provider", provider.Name).
			Str("model", model).
			Str("endpoint", endpoint).
			Msg("HTTP request failed")
		db.LogError(provider.Name, "http_error", err.Error())
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	var respReader io.Reader = resp.Body
	if TraceMode {
		respBody, _ := io.ReadAll(resp.Body)
		logger.Info().
			Str("provider", provider.Name).
			Int("status", resp.StatusCode).
			Str("response", string(respBody)).
			Msg("TRACE: Upstream Response")
		// Replace body so checkResponseStatus and decodeResponse can read it
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		respReader = bytes.NewReader(respBody)
	}

	if err := checkResponseStatus(resp, provider); err != nil {
		return nil, resp.StatusCode, err
	}

	respObj, decodeErr := decodeResponse(provider, respReader)
	return respObj, resp.StatusCode, decodeErr
}

// forwardStreamRequest forwards a streaming request and returns status code
func forwardStreamRequest(ctx context.Context, provider config.Provider, model string, req Request, w http.ResponseWriter) (int, error) {
	startTime := time.Now()
	endpoint := buildEndpoint(provider, "/chat/completions")
	body, err := buildRequestBody(provider, model, req)
	if err != nil {
		logger.Error().Err(err).Str("provider", provider.Name).Msg("Failed to build stream request body")
		return 0, err
	}

	if TraceMode {
		logger.Info().
			Str("provider", provider.Name).
			Str("model", model).
			Str("endpoint", endpoint).
			Str("payload", string(body)).
			Msg("TRACE: Upstream Stream Request")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		logger.Error().Err(err).Str("provider", provider.Name).Str("endpoint", endpoint).Msg("Failed to create stream HTTP request")
		return 0, err
	}

	setStreamRequestHeaders(httpReq, provider)

	resp, err := streamClient.Do(httpReq)
	if err != nil {
		logger.Error().
			Err(err).
			Str("provider", provider.Name).
			Str("model", model).
			Msg("Stream HTTP request failed")
		db.LogError(provider.Name, "http_error", err.Error())
		latency := time.Since(startTime).Milliseconds()
		GlobalMetricsStore.ReportRequest(provider.Name, latency, err)
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if TraceMode {
		logger.Info().
			Str("provider", provider.Name).
			Int("status", resp.StatusCode).
			Msg("TRACE: Upstream Stream Response Status")
	}

	if resp.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(resp.Body)
		logger.Error().
			Str("provider", provider.Name).
			Str("model", model).
			Int("status", resp.StatusCode).
			Str("response", string(respBody)).
			Msg("Stream API error")
		db.LogError(provider.Name, "api_error", string(respBody))
		err = fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
		latency := time.Since(startTime).Milliseconds()
		GlobalMetricsStore.ReportRequest(provider.Name, latency, err)
		return resp.StatusCode, err
	}

	err = streamResponse(provider, resp, w)
	latency := time.Since(startTime).Milliseconds()
	GlobalMetricsStore.ReportRequest(provider.Name, latency, err)
	return resp.StatusCode, err
}

// buildEndpoint builds the endpoint URL for a provider
func buildEndpoint(provider config.Provider, path string) string {
	baseURL := trimTrailingSlash(provider.BaseURL)
	if provider.IsNativeOllama() {
		baseURL = trimSuffix(baseURL, "/api")
		if path == "/chat/completions" {
			return baseURL + "/api/chat"
		}
		return baseURL + "/api/tags"
	}
	return baseURL + path
}

// buildRequestBody builds the request body for a provider
func buildRequestBody(provider config.Provider, model string, req Request) ([]byte, error) {
	if provider.IsNativeOllama() {
		return buildOllamaBody(model, req)
	}
	body, err := buildOpenAIBody(model, req)
	if err != nil {
		return nil, err
	}

	// Only unmarshal and adjust if the provider requires specific fixes
	if provider.Name == "groq" || provider.Name == "ollama" || provider.Name == "Mistral" || provider.Name == "OMNI" {
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}

		adjustProviderPayload(provider.Name, payload)

		adjustedBody, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return nil, marshalErr
		}
		return adjustedBody, nil
	}

	return body, nil
}

// adjustProviderPayload handles all provider-specific parameter adjustments
func adjustProviderPayload(providerName string, payload map[string]interface{}) {
	// 1. Groq specific reasoning_effort adjustment
	if providerName == "groq" {
		adjustGroqReasoningEffort(payload)
	}

	// 2. Common parameter filtering for non-OpenAI-native providers
	filterCommonUnsupportedParams(providerName, payload)
}

// adjustGroqReasoningEffort ensures reasoning_effort is compatible with Groq
func adjustGroqReasoningEffort(payload map[string]interface{}) {
	// Some Groq models don't support reasoning_effort at all
	delete(payload, "reasoning_effort")
}

// filterCommonUnsupportedParams removes parameters that cause 422/400 errors
func filterCommonUnsupportedParams(providerName string, payload map[string]interface{}) {
	// These fields are OpenAI-specific and often cause failures elsewhere
	delete(payload, "store")
	delete(payload, "max_completion_tokens")

	// Some providers are strict about reasoning_effort
	if providerName == "Mistral" || providerName == "ollama" || providerName == "OMNI" {
		delete(payload, "reasoning_effort")
	}
}

// buildOllamaBody builds Ollama-compatible request body (native /api/chat)
func buildOllamaBody(model string, req Request) ([]byte, error) {
	fullPayload := buildFullPayload(req)

	ollamaPayload := map[string]interface{}{
		"model":  model,
		"stream": req.Stream,
	}

	// Required: messages
	if messages, ok := fullPayload["messages"]; ok {
		ollamaPayload["messages"] = messages
	}

	// Optional: tools
	if tools, ok := fullPayload["tools"]; ok {
		ollamaPayload["tools"] = tools
	}

	// Handle response_format (OpenAI response_format -> Ollama format)
	if respFormat, ok := fullPayload["response_format"].(map[string]interface{}); ok {
		if t, ok := respFormat["type"].(string); ok && t == "json_object" {
			ollamaPayload["format"] = "json"
		}
	} else if fmtVal, ok := fullPayload["format"].(string); ok {
		ollamaPayload["format"] = fmtVal
	}

	// Create options object for hyperparameters
	options := make(map[string]interface{})

	// Hyperparameters mapping (OpenAI -> Ollama)
	hyperParams := map[string]string{
		"temperature":    "temperature",
		"top_p":          "top_p",
		"top_k":          "top_k",
		"seed":           "seed",
		"stop":           "stop",
		"num_ctx":        "num_ctx",
		"repeat_penalty": "repeat_penalty",
		"mirostat":       "mirostat",
		"mirostat_eta":   "mirostat_eta",
		"mirostat_tau":   "mirostat_tau",
		"tfs_z":          "tfs_z",
	}

	for openaiKey, ollamaKey := range hyperParams {
		if val, ok := fullPayload[openaiKey]; ok {
			options[ollamaKey] = val
		}
	}

	// Map tokens limit (max_tokens/max_completion_tokens -> num_predict)
	if maxTokens, ok := fullPayload["max_tokens"]; ok {
		options["num_predict"] = maxTokens
	} else if maxCompTokens, ok := fullPayload["max_completion_tokens"]; ok {
		options["num_predict"] = maxCompTokens
	}

	if len(options) > 0 {
		ollamaPayload["options"] = options
	}

	// Keep keep_alive in root if present
	if keepAlive, ok := fullPayload["keep_alive"]; ok {
		ollamaPayload["keep_alive"] = keepAlive
	}

	return json.Marshal(ollamaPayload)
}

// buildOpenAIBody builds OpenAI-compatible request body
func buildOpenAIBody(model string, req Request) ([]byte, error) {
	payload := buildFullPayload(req)
	payload["model"] = model
	return json.Marshal(payload)
}

// setRequestHeaders sets headers for a standard request
func setRequestHeaders(req *http.Request, provider config.Provider) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Provider", provider.Name)

	if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}

	if !provider.IsNativeOllama() {
		req.Header.Set("HTTP-Referer", "http://localhost:3000")
		req.Header.Set("X-Title", "AI Revolver Proxy")
	}
}

// setStreamRequestHeaders sets headers for a streaming request
func setStreamRequestHeaders(req *http.Request, provider config.Provider) {
	setRequestHeaders(req, provider)
	if !provider.IsNativeOllama() {
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Connection", "keep-alive")
	}
}

// checkResponseStatus checks the response status code
func checkResponseStatus(resp *http.Response, provider config.Provider) error {
	if resp.StatusCode == http.StatusTooManyRequests {
		logger.Warn().
			Str("provider", provider.Name).
			Msg("Rate limit exceeded")
		db.LogRateLimit(provider.Name, time.Now().Add(60*time.Second))
		return fmt.Errorf("rate limit exceeded for provider %s", provider.Name)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(resp.Body)
		logger.Error().
			Str("provider", provider.Name).
			Int("status", resp.StatusCode).
			Str("response", string(respBody)).
			Msg("API error response")
		db.LogError(provider.Name, "api_error", string(respBody))
		return fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// decodeResponse decodes the response body
func decodeResponse(provider config.Provider, body io.Reader) (*Response, error) {
	if provider.IsNativeOllama() {
		return decodeOllamaResponse(body)
	}

	var result Response
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		logger.Error().
			Err(err).
			Str("provider", provider.Name).
			Msg("Failed to decode response")
		return nil, err
	}
	return &result, nil
}

// decodeOllamaResponse decodes an Ollama response
func decodeOllamaResponse(body io.Reader) (*Response, error) {
	var ollamaResp OllamaResponse

	if err := json.NewDecoder(body).Decode(&ollamaResp); err != nil {
		logger.Error().Err(err).Msg("Failed to decode Ollama response")
		return nil, err
	}

	var toolCalls json.RawMessage
	if ollamaResp.Message.ToolCalls != nil {
		toolCalls, _ = json.Marshal(ollamaResp.Message.ToolCalls)
	}

	return &Response{
		Model: ollamaResp.Model,
		Choices: []Choice{
			{
				Index: 0,
				Message: Message{
					Role:      ollamaResp.Message.Role,
					Content:   ollamaResp.Message.Content,
					ToolCalls: toolCalls,
				},
				FinishReason: "stop",
			},
		},
		Usage: &Usage{
			PromptTokens:     ollamaResp.PromptEvalCount,
			CompletionTokens: ollamaResp.EvalCount,
			TotalTokens:      ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		},
	}, nil
}

// streamResponse streams the response to the client
func streamResponse(provider config.Provider, resp *http.Response, w http.ResponseWriter) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	if provider.IsNativeOllama() {
		return streamOllamaResponse(resp, w, flusher)
	}

	return streamOpenAIResponse(resp, w, flusher)
}

// streamOllamaResponse streams an Ollama NDJSON response as SSE
func streamOllamaResponse(resp *http.Response, w http.ResponseWriter, flusher http.Flusher) error {
	scanner := bufio.NewScanner(resp.Body)
	// Use configurable buffer size
	scanner.Buffer(make([]byte, OllamaStreamBufferSize), int(OllamaStreamBufferSize))

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		if TraceMode {
			logger.Info().Str("chunk", line).Msg("TRACE: Ollama Stream Chunk")
		}

		var chunk OllamaResponse

		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			logger.Debug().Err(err).Str("line", line).Msg("Failed to parse Ollama chunk")
			continue
		}

		delta := map[string]interface{}{
			"content": chunk.Message.Content,
		}
		if chunk.Message.ToolCalls != nil {
			delta["tool_calls"] = chunk.Message.ToolCalls
		}

		sseChunk := map[string]interface{}{
			"model": chunk.Model,
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"delta": delta,
				},
			},
		}

		if chunk.Done {
			sseChunk["choices"].([]map[string]interface{})[0]["finish_reason"] = "stop"
			sseChunk["usage"] = map[string]interface{}{
				"prompt_tokens":     chunk.PromptEvalCount,
				"completion_tokens": chunk.EvalCount,
				"total_tokens":      chunk.PromptEvalCount + chunk.EvalCount,
			}
		}

		chunkJSON, _ := json.Marshal(sseChunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", chunkJSON)
		flusher.Flush()

		if chunk.Done {
			_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			break
		}
	}

	return scanner.Err()
}

// streamOpenAIResponse streams a standard SSE response
func streamOpenAIResponse(resp *http.Response, w http.ResponseWriter, flusher http.Flusher) error {
	// Check if response is actually SSE or regular JSON
	contentType := resp.Header.Get("Content-Type")

	if contentType == "application/json" || contentType == "application/json; charset=utf-8" {
		// Provider returned JSON instead of SSE - handle as single response
		var result Response
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return err
		}

		// Convert to SSE format and send
		if len(result.Choices) > 0 {
			msg := result.Choices[0].Message
			delta := map[string]interface{}{
				"content": msg.GetContentString(),
			}

			if msg.ToolCalls != nil {
				delta["tool_calls"] = msg.ToolCalls
			}
			if msg.ToolCallID != "" {
				delta["tool_call_id"] = msg.ToolCallID
			}

			chunk := map[string]interface{}{
				"choices": []map[string]interface{}{
					{"delta": delta},
				},
			}
			chunkJSON, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", chunkJSON)
			flusher.Flush()
		}

		return nil
	}

	// Handle as SSE stream - use efficient line-by-line copying
	scanner := bufio.NewScanner(resp.Body)
	// Use configurable buffer size
	scanner.Buffer(make([]byte, OpenAIStreamBufferSize), int(OpenAIStreamBufferSize))

	for scanner.Scan() {
		line := scanner.Text()
		if _, err := w.Write([]byte(line + "\n")); err != nil {
			// Client disconnected - this is normal, don't log as error
			return nil
		}
		// Only flush on SSE data lines (lines starting with "data:") or empty lines
		if len(line) == 0 || len(line) >= 5 && line[:5] == "data:" {
			flusher.Flush()
		}
	}

	return scanner.Err()
}

// getProviderAndModel finds the provider and model for a request
func getProviderAndModel(ctx context.Context, req Request) (*config.Provider, string, error) {
	cfg := config.GetConfig()

	if req.Model == "auto" {
		candidates := getAllCandidates(cfg)
		if len(candidates) == 0 {
			return nil, "", fmt.Errorf("no providers available for auto mode")
		}
		// Return the first candidate as it's already sorted by tier/priority
		return &candidates[0].Provider, candidates[0].Model, nil
	}

	if req.Provider != "" {
		return findModelInProvider(ctx, cfg, req.Provider, req.Model)
	}

	return findModelAnywhere(ctx, req.Model)
}

// findModelInProvider finds a model in a specific provider
func findModelInProvider(_ context.Context, cfg config.Config, providerName, modelName string) (*config.Provider, string, error) {
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == providerName {
			for _, model := range cfg.Providers[i].Models {
				if model.Name == modelName {
					return &cfg.Providers[i], modelName, nil
				}
			}
		}
	}
	return nil, "", fmt.Errorf("model %s not found in provider %s", modelName, providerName)
}

// findModelAnywhere finds a model in any provider
func findModelAnywhere(_ context.Context, modelName string) (*config.Provider, string, error) {
	cfg := config.GetConfig()
	for i := range cfg.Providers {
		for _, model := range cfg.Providers[i].Models {
			if model.Name == modelName {
				return &cfg.Providers[i], modelName, nil
			}
		}
	}
	return nil, "", fmt.Errorf("model %s not found", modelName)
}

// buildFullPayload builds a complete payload by merging raw body and known fields
func buildFullPayload(req Request) map[string]interface{} {
	payload := make(map[string]interface{})

	if len(req.RawBody) > 0 {
		var original map[string]interface{}
		if err := json.Unmarshal(req.RawBody, &original); err == nil {
			for k, v := range original {
				if !KnownRequestFields[k] {
					payload[k] = v
				}
			}
		}
	}

	if req.Model != "" {
		payload["model"] = req.Model
	}

	if len(req.Messages) > 0 {
		payload["messages"] = transformMessages(req.Messages)
	}

	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		payload["top_p"] = req.TopP
	}

	if len(req.Tools) > 0 {
		payload["tools"] = req.Tools
	}
	if req.ToolChoice != nil {
		payload["tool_choice"] = req.ToolChoice
	}

	if req.ExtraParams != nil {
		for k, v := range req.ExtraParams {
			payload[k] = v
		}
	}

	return payload
}

// transformMessages converts messages to upstream API format
func transformMessages(messages []Message) []map[string]interface{} {
	result := make([]map[string]interface{}, len(messages))
	for i, msg := range messages {
		msgMap := map[string]interface{}{
			"role":    msg.Role,
			"content": msg.GetContentString(),
		}
		if msg.ToolCalls != nil {
			msgMap["tool_calls"] = msg.ToolCalls
		}
		if msg.ToolCallID != "" {
			msgMap["tool_call_id"] = msg.ToolCallID
		}
		result[i] = msgMap
	}
	return result
}

// trimTrailingSlash removes trailing slashes from URL
func trimTrailingSlash(url string) string {
	for len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}
	return url
}

// trimSuffix removes suffix from string if present
func trimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}
