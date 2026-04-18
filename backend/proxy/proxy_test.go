package proxy

import (
	"ai-proxy/config"
	"encoding/json"
	"fmt"
	"testing"
)

func TestGetContentString(t *testing.T) {
	tests := []struct {
		name     string
		content  interface{}
		expected string
	}{
		{
			name:     "nil content",
			content:  nil,
			expected: "",
		},
		{
			name:     "string content",
			content:  "hello world",
			expected: "hello world",
		},
		{
			name:     "empty string",
			content:  "",
			expected: "",
		},
		{
			name:     "Anthropic format array",
			content:  []interface{}{map[string]interface{}{"type": "text", "text": "hello"}, map[string]interface{}{"type": "text", "text": " world"}},
			expected: "hello world",
		},
		{
			name:     "Anthropic format with non-text block",
			content:  []interface{}{map[string]interface{}{"type": "image", "url": "http://example.com"}, map[string]interface{}{"type": "text", "text": "see image"}},
			expected: "see image",
		},
		{
			name:     "int content",
			content:  42,
			expected: "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := Message{Role: "user", Content: tt.content}
			result := msg.GetContentString()
			if result != tt.expected {
				t.Errorf("GetContentString() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestBuildFullPayload(t *testing.T) {
	t.Run("basic fields", func(t *testing.T) {
		req := Request{
			Model:       "test-model",
			MaxTokens:   100,
			Temperature: 0.7,
			TopP:        0.9,
			Messages: []Message{
				{Role: "user", Content: "hello"},
			},
		}

		payload := buildFullPayload(req)

		if payload["model"] != "test-model" {
			t.Errorf("Expected model 'test-model', got %v", payload["model"])
		}
		if payload["max_tokens"] != 100 {
			t.Errorf("Expected max_tokens 100, got %v", payload["max_tokens"])
		}
		if payload["temperature"] != 0.7 {
			t.Errorf("Expected temperature 0.7, got %v", payload["temperature"])
		}
		if payload["top_p"] != 0.9 {
			t.Errorf("Expected top_p 0.9, got %v", payload["top_p"])
		}
	})

	t.Run("with raw body and extra params", func(t *testing.T) {
		rawBody, _ := json.Marshal(map[string]interface{}{
			"model":    "original-model",
			"messages": []interface{}{},
			"tools":    []interface{}{map[string]interface{}{"name": "test"}},
		})

		req := Request{
			Model:   "override-model",
			RawBody: rawBody,
			Tools:   []interface{}{map[string]interface{}{"name": "test"}},
			ExtraParams: map[string]interface{}{
				"custom_param": "value",
			},
		}

		payload := buildFullPayload(req)

		if payload["model"] != "override-model" {
			t.Errorf("Expected model 'override-model', got %v", payload["model"])
		}
		if payload["tools"] == nil {
			t.Error("Expected tools to be preserved from raw body")
		}
		if payload["custom_param"] != "value" {
			t.Errorf("Expected custom_param 'value', got %v", payload["custom_param"])
		}
	})

	t.Run("empty values omitted", func(t *testing.T) {
		req := Request{
			Model: "test",
		}

		payload := buildFullPayload(req)

		if _, ok := payload["max_tokens"]; ok {
			t.Error("max_tokens should not be present when 0")
		}
		if _, ok := payload["temperature"]; ok {
			t.Error("temperature should not be present when 0")
		}
		if _, ok := payload["top_p"]; ok {
			t.Error("top_p should not be present when 0")
		}
	})
}

func TestFilterByModel(t *testing.T) {
	candidates := []ProviderModelPair{
		{Provider: testProvider("p1"), Model: "model-a"},
		{Provider: testProvider("p2"), Model: "model-b"},
		{Provider: testProvider("p1"), Model: "model-a"},
		{Provider: testProvider("p2"), Model: "model-c"},
	}

	filtered := filterByModel(candidates, "model-a")

	if len(filtered) != 2 {
		t.Errorf("Expected 2 candidates for model-a, got %d", len(filtered))
	}

	for _, c := range filtered {
		if c.Model != "model-a" {
			t.Errorf("Expected model-a, got %s", c.Model)
		}
	}
}

func TestTrimTrailingSlash(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"http://example.com/", "http://example.com"},
		{"http://example.com///", "http://example.com"},
		{"http://example.com", "http://example.com"},
		{"", ""},
		{"/", ""},
	}

	for _, tt := range tests {
		result := trimTrailingSlash(tt.input)
		if result != tt.expected {
			t.Errorf("trimTrailingSlash(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestTrimSuffix(t *testing.T) {
	tests := []struct {
		s        string
		suffix   string
		expected string
	}{
		{"http://example.com/api", "/api", "http://example.com"},
		{"http://example.com", "/api", "http://example.com"},
		{"hello world", " world", "hello"},
		{"", "/api", ""},
	}

	for _, tt := range tests {
		result := trimSuffix(tt.s, tt.suffix)
		if result != tt.expected {
			t.Errorf("trimSuffix(%q, %q) = %q, want %q", tt.s, tt.suffix, result, tt.expected)
		}
	}
}

func TestTransformMessages(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there", ToolCallID: "call_123"},
	}

	result := transformMessages(messages)

	if len(result) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(result))
	}

	if result[0]["role"] != "user" {
		t.Errorf("Expected role 'user', got %v", result[0]["role"])
	}
	if result[0]["content"] != "hello" {
		t.Errorf("Expected content 'hello', got %v", result[0]["content"])
	}

	if result[1]["role"] != "assistant" {
		t.Errorf("Expected role 'assistant', got %v", result[1]["role"])
	}
	if result[1]["tool_call_id"] != "call_123" {
		t.Errorf("Expected tool_call_id 'call_123', got %v", result[1]["tool_call_id"])
	}
}

func TestSortedProviders(t *testing.T) {
	providers := []config.Provider{
		{Name: "low", Priority: 3, Enabled: BoolPtr(true)},
		{Name: "high", Priority: 1, Enabled: BoolPtr(true)},
		{Name: "medium", Priority: 2, Enabled: BoolPtr(true)},
	}

	sorted := sortedProviders(providers)

	if sorted[0].Name != "high" {
		t.Errorf("Expected first provider to be 'high', got '%s'", sorted[0].Name)
	}
	if sorted[1].Name != "medium" {
		t.Errorf("Expected second provider to be 'medium', got '%s'", sorted[1].Name)
	}
	if sorted[2].Name != "low" {
		t.Errorf("Expected third provider to be 'low', got '%s'", sorted[2].Name)
	}
}

func TestFindMaxModels(t *testing.T) {
	providers := []config.Provider{
		{Models: []config.Model{{Name: "a"}, {Name: "b"}, {Name: "c"}}},
		{Models: []config.Model{{Name: "x"}}},
		{Models: []config.Model{{Name: "y"}, {Name: "z"}}},
	}

	maxVal := findMaxModels(providers)
	if maxVal != 3 {
		t.Errorf("Expected max models to be 3, got %d", maxVal)
	}
}

func TestFormatAllProvidersFailedError(t *testing.T) {
	t.Run("with last error", func(t *testing.T) {
		err := formatAllProvidersFailedError(3, fmt.Errorf("connection refused"))
		expected := "all providers failed after 3 attempts, last error: connection refused"
		if err.Error() != expected {
			t.Errorf("Expected '%s', got '%s'", expected, err.Error())
		}
	})

	t.Run("without last error", func(t *testing.T) {
		err := formatAllProvidersFailedError(0, nil)
		expected := "all providers failed"
		if err.Error() != expected {
			t.Errorf("Expected '%s', got '%s'", expected, err.Error())
		}
	})
}

func TestIsRateLimited(t *testing.T) {
	cfg := config.Config{
		Providers: []config.Provider{
			{Name: "normal", CurrentUsage: 50, RateLimit: 100},
			{Name: "limited", CurrentUsage: 100, RateLimit: 100},
			{Name: "over", CurrentUsage: 150, RateLimit: 100},
		},
	}

	tests := []struct {
		provider config.Provider
		expected bool
	}{
		{cfg.Providers[0], false},
		{cfg.Providers[1], true},
		{cfg.Providers[2], true},
	}

	for _, tt := range tests {
		result := isRateLimited(cfg, tt.provider)
		if result != tt.expected {
			t.Errorf("isRateLimited(%s) = %v, want %v", tt.provider.Name, result, tt.expected)
		}
	}
}

// Helper to create Provider from name
func testProvider(name string) config.Provider {
	return config.Provider{Name: name}
}
