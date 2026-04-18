package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// MockResponseConfig defines how the mock should respond
type MockResponseConfig struct {
	Model        string
	ErrorMessage string
	Latency      time.Duration
	StatusCode   int
	FailCount    int
	BlockAfter   int
	Success      bool
}

// MockServer manages a mock server for testing
type MockServer struct {
	Server    *httptest.Server
	Responses map[string][]MockResponseConfig
	counters  map[string]int
	counter   int
	mu        sync.Mutex
}

// NewMockServer creates a new mock server
func NewMockServer() *MockServer {
	return &MockServer{
		Responses: make(map[string][]MockResponseConfig),
		counters:  make(map[string]int),
	}
}

// AddResponse adds a response for a provider
func (m *MockServer) AddResponse(provider string, responses ...MockResponseConfig) {
	m.Responses[provider] = responses
}

// Handler возвращает HTTP handler для мок-сервера
func (m *MockServer) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.counter++
		counter := m.counter
		m.mu.Unlock()

		// Apply latency first
		select {
		case <-time.After(50 * time.Millisecond): // Small base latency
		case <-r.Context().Done():
			return
		}

		resp := m.selectResponse(r, counter)

		// Apply custom latency if specified
		if resp.Latency > 0 {
			select {
			case <-time.After(resp.Latency):
			case <-r.Context().Done():
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")

		// Check if should return error
		if !resp.Success || (resp.FailCount > 0 && counter <= resp.FailCount) {
			m.writeError(w, resp)
			return
		}

		m.writeSuccess(w)
	})
}

func (m *MockServer) selectResponse(r *http.Request, counter int) MockResponseConfig {
	provider := r.Header.Get("X-Provider")
	found := false
	var resp MockResponseConfig

	if provider != "" {
		m.mu.Lock()
		if responses, ok := m.Responses[provider]; ok && len(responses) > 0 {
			idx := (counter - 1) % len(responses)
			resp = responses[idx]
			found = true
			m.counters[provider]++
		}
		m.mu.Unlock()
	}

	if !found {
		m.mu.Lock()
		// Fall back to first provider with responses (round-robin style)
		for p, responses := range m.Responses {
			if len(responses) > 0 {
				idx := (counter - 1) % len(responses)
				resp = responses[idx]
				found = true
				m.counters[p]++
				break
			}
		}
		m.mu.Unlock()
	}

	if !found {
		resp = MockResponseConfig{Success: true}
	}
	return resp
}

func (m *MockServer) writeError(w http.ResponseWriter, resp MockResponseConfig) {
	if resp.StatusCode > 0 {
		w.WriteHeader(resp.StatusCode)
	} else {
		w.WriteHeader(500)
	}
	if resp.ErrorMessage != "" {
		_, _ = w.Write([]byte(`{"error": "` + resp.ErrorMessage + `"}`))
	} else {
		_, _ = w.Write([]byte(`{"error": "mock error"}`))
	}
}

func (m *MockServer) writeSuccess(w http.ResponseWriter) {
	w.WriteHeader(200)
	// OpenAI format response
	response := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "Mock response",
				},
			},
		},
	}
	_ = json.NewEncoder(w).Encode(response)
}

// Start starts the mock server
func (m *MockServer) Start() string {
	m.Server = httptest.NewServer(m.Handler())
	return m.Server.URL
}

// Stop stops the mock server
func (m *MockServer) Stop() {
	if m.Server != nil {
		m.Server.Close()
	}
}

// Reset resets all counters
func (m *MockServer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counter = 0
	for k := range m.counters {
		m.counters[k] = 0
	}
}

// GetCounter returns the total number of requests
func (m *MockServer) GetCounter() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counter
}

// GetRequestCount returns the request count for a specific provider
func (m *MockServer) GetRequestCount(provider string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[provider]
}

// MockServerForProvider creates a mock server for a single provider
type MockServerForProvider struct {
	Server   *httptest.Server
	Provider string
	Response MockResponseConfig
	mu       sync.Mutex
	counter  int
}

// NewMockServerForProvider creates a mock for a single provider
func NewMockServerForProvider(provider string, response MockResponseConfig) *MockServerForProvider {
	m := &MockServerForProvider{
		Provider: provider,
		Response: response,
	}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handler))
	return m
}

func (m *MockServerForProvider) handler(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.counter++
	count := m.counter
	m.mu.Unlock()

	// Apply latency
	if m.Response.Latency > 0 {
		select {
		case <-time.After(m.Response.Latency):
		case <-r.Context().Done():
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")

	// Check if should return error (either !Success or FailCount exceeded)
	shouldFail := !m.Response.Success
	if m.Response.FailCount > 0 && count <= m.Response.FailCount {
		shouldFail = true
	}

	if shouldFail {
		statusCode := m.Response.StatusCode
		if statusCode == 0 {
			statusCode = 500
		}
		w.WriteHeader(statusCode)
		if m.Response.ErrorMessage != "" {
			_, _ = w.Write([]byte(`{"error": "` + m.Response.ErrorMessage + `"}`))
		} else {
			_, _ = w.Write([]byte(`{"error": "mock error"}`))
		}
		return
	}

	w.WriteHeader(200)
	response := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "Mock response from " + m.Provider,
				},
			},
		},
	}
	_ = json.NewEncoder(w).Encode(response)
}

// URL returns the mock server's base URL
func (m *MockServerForProvider) URL() string {
	return m.Server.URL
}

// Stop stops the mock server
func (m *MockServerForProvider) Stop() {
	m.Server.Close()
}

// Counter returns the number of requests received
func (m *MockServerForProvider) Counter() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counter
}

// Reset resets the request counter
func (m *MockServerForProvider) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counter = 0
}

// MultiMockServer creates multiple mock servers for different providers
type MultiMockServer struct {
	Servers map[string]*MockServerForProvider
}

// NewMultiMockServer creates a new multi-mock server
func NewMultiMockServer() *MultiMockServer {
	return &MultiMockServer{
		Servers: make(map[string]*MockServerForProvider),
	}
}

// Add adds a mock for a provider
func (m *MultiMockServer) Add(provider string, response MockResponseConfig) *MultiMockServer {
	m.Servers[provider] = NewMockServerForProvider(provider, response)
	return m
}

// URLFor returns the URL for a specific provider
func (m *MultiMockServer) URLFor(provider string) string {
	if s, ok := m.Servers[provider]; ok {
		return s.URL()
	}
	return ""
}

// Stop stops all mock servers
func (m *MultiMockServer) Stop() {
	for _, s := range m.Servers {
		s.Stop()
	}
}

// Counter returns the number of requests for a provider
func (m *MultiMockServer) Counter(provider string) int {
	if s, ok := m.Servers[provider]; ok {
		return s.Counter()
	}
	return 0
}

// GetRequestCount returns the request count for a provider (alias for Counter)
func (m *MultiMockServer) GetRequestCount(provider string) int {
	return m.Counter(provider)
}

// Reset resets all counters
func (m *MultiMockServer) Reset() {
	for _, s := range m.Servers {
		s.Reset()
	}
}
