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

func testProvider(name string) config.Provider {
	return config.Provider{Name: name}
}

func TestBuildEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		provider config.Provider
		path     string
		expected string
	}{
		{
			name:     "standard OpenAI provider",
			provider: config.Provider{Name: "openai", BaseURL: "https://api.openai.com/v1"},
			path:     "/chat/completions",
			expected: "https://api.openai.com/v1/chat/completions",
		},
		{
			name:     "provider with trailing slash",
			provider: config.Provider{Name: "openai", BaseURL: "https://api.openai.com/v1/"},
			path:     "/models",
			expected: "https://api.openai.com/v1/models",
		},
		{
			name:     "native ollama chat",
			provider: config.Provider{Name: "ollama", BaseURL: "http://localhost:11434"},
			path:     "/chat/completions",
			expected: "http://localhost:11434/api/chat",
		},
		{
			name:     "native ollama models",
			provider: config.Provider{Name: "ollama", BaseURL: "http://localhost:11434"},
			path:     "/models",
			expected: "http://localhost:11434/api/tags",
		},
		{
			name:     "native ollama with /api suffix in base",
			provider: config.Provider{Name: "ollama", BaseURL: "http://localhost:11434/api"},
			path:     "/chat/completions",
			expected: "http://localhost:11434/api/chat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildEndpoint(tt.provider, tt.path)
			if got != tt.expected {
				t.Errorf("buildEndpoint() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestAdjustGroqReasoningEffort(t *testing.T) {
	payload := map[string]interface{}{
		"model":            "test",
		"reasoning_effort": "high",
		"other":            "value",
	}

	adjustGroqReasoningEffort(payload)

	if _, ok := payload["reasoning_effort"]; ok {
		t.Error("reasoning_effort should have been removed")
	}
	if payload["other"] != "value" {
		t.Error("other fields should be preserved")
	}
}

func TestFilterCommonUnsupportedParams(t *testing.T) {
	t.Run("removes store and max_completion_tokens for all providers", func(t *testing.T) {
		payload := map[string]interface{}{
			"store":                "true",
			"max_completion_tokens": 1000,
			"model":                "test",
		}
		filterCommonUnsupportedParams("openrouter", payload)
		if _, ok := payload["store"]; ok {
			t.Error("store should have been removed")
		}
		if _, ok := payload["max_completion_tokens"]; ok {
			t.Error("max_completion_tokens should have been removed")
		}
		if payload["model"] != "test" {
			t.Error("model should be preserved")
		}
	})

	t.Run("removes reasoning_effort for Mistral", func(t *testing.T) {
		payload := map[string]interface{}{"reasoning_effort": "high"}
		filterCommonUnsupportedParams("Mistral", payload)
		if _, ok := payload["reasoning_effort"]; ok {
			t.Error("reasoning_effort should be removed for Mistral")
		}
	})

	t.Run("removes reasoning_effort for ollama", func(t *testing.T) {
		payload := map[string]interface{}{"reasoning_effort": "medium"}
		filterCommonUnsupportedParams("ollama", payload)
		if _, ok := payload["reasoning_effort"]; ok {
			t.Error("reasoning_effort should be removed for ollama")
		}
	})

	t.Run("preserves reasoning_effort for groq (handled separately)", func(t *testing.T) {
		payload := map[string]interface{}{"reasoning_effort": "low"}
		filterCommonUnsupportedParams("groq", payload)
		// groq reasoning_effort is removed by adjustGroqReasoningEffort, not here
		if _, ok := payload["reasoning_effort"]; !ok {
			t.Error("reasoning_effort should still be present after filterCommonUnsupportedParams for groq")
		}
	})
}

func TestAdjustProviderPayload(t *testing.T) {
	t.Run("groq removes reasoning_effort and common params", func(t *testing.T) {
		payload := map[string]interface{}{
			"reasoning_effort":      "high",
			"store":                 "true",
			"max_completion_tokens": 500,
			"model":                 "test",
		}
		adjustProviderPayload("groq", payload)
		if _, ok := payload["reasoning_effort"]; ok {
			t.Error("reasoning_effort should be removed for groq")
		}
		if _, ok := payload["store"]; ok {
			t.Error("store should be removed")
		}
	})

	t.Run("Mistral removes reasoning_effort", func(t *testing.T) {
		payload := map[string]interface{}{
			"reasoning_effort": "high",
			"model":            "mistral-large",
		}
		adjustProviderPayload("Mistral", payload)
		if _, ok := payload["reasoning_effort"]; ok {
			t.Error("reasoning_effort should be removed for Mistral")
		}
	})
}

func TestBuildOllamaBody(t *testing.T) {
	t.Run("basic request", func(t *testing.T) {
		req := Request{
			Messages:  []Message{{Role: "user", Content: "hello"}},
			Stream:    false,
			MaxTokens: 200,
		}
		body, err := buildOllamaBody("llama3", req)
		if err != nil {
			t.Fatalf("buildOllamaBody() error = %v", err)
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Failed to unmarshal ollama body: %v", err)
		}

		if payload["model"] != "llama3" {
			t.Errorf("Expected model 'llama3', got %v", payload["model"])
		}
		if payload["stream"] != false {
			t.Errorf("Expected stream false, got %v", payload["stream"])
		}
		opts, ok := payload["options"].(map[string]interface{})
		if !ok {
			t.Error("Expected options map")
		} else {
			if opts["num_predict"] != float64(200) {
				t.Errorf("Expected num_predict 200, got %v", opts["num_predict"])
			}
		}
	})

	t.Run("with json response_format", func(t *testing.T) {
		rawBody, _ := json.Marshal(map[string]interface{}{
			"response_format": map[string]interface{}{"type": "json_object"},
		})
		req := Request{
			Messages: []Message{{Role: "user", Content: "json please"}},
			RawBody:  rawBody,
		}
		body, err := buildOllamaBody("llama3", req)
		if err != nil {
			t.Fatalf("buildOllamaBody() error = %v", err)
		}

		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Failed to unmarshal ollama body: %v", err)
		}
		if payload["format"] != "json" {
			t.Errorf("Expected format 'json', got %v", payload["format"])
		}
	})

	t.Run("with keep_alive", func(t *testing.T) {
		rawBody, _ := json.Marshal(map[string]interface{}{
			"keep_alive": "5m",
		})
		req := Request{
			Messages: []Message{{Role: "user", Content: "hi"}},
			RawBody:  rawBody,
		}
		body, err := buildOllamaBody("llama3", req)
		if err != nil {
			t.Fatalf("buildOllamaBody() error = %v", err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Failed to unmarshal ollama body: %v", err)
		}
		if payload["keep_alive"] != "5m" {
			t.Errorf("Expected keep_alive '5m', got %v", payload["keep_alive"])
		}
	})

	t.Run("with tools", func(t *testing.T) {
		req := Request{
			Messages: []Message{{Role: "user", Content: "use tool"}},
			Tools:    []interface{}{map[string]interface{}{"name": "search"}},
		}
		body, err := buildOllamaBody("llama3", req)
		if err != nil {
			t.Fatalf("buildOllamaBody() error = %v", err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Failed to unmarshal ollama body: %v", err)
		}
		if payload["tools"] == nil {
			t.Error("Expected tools to be present")
		}
	})
}

func TestBuildRequestBody(t *testing.T) {
	t.Run("standard provider returns OpenAI body", func(t *testing.T) {
		provider := config.Provider{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1"}
		req := Request{
			Messages: []Message{{Role: "user", Content: "hello"}},
		}
		body, err := buildRequestBody(provider, "test-model", req)
		if err != nil {
			t.Fatalf("buildRequestBody() error = %v", err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Failed to unmarshal body: %v", err)
		}
		if payload["model"] != "test-model" {
			t.Errorf("Expected model 'test-model', got %v", payload["model"])
		}
	})

	t.Run("groq provider filters params", func(t *testing.T) {
		provider := config.Provider{Name: "groq"}
		rawBody, _ := json.Marshal(map[string]interface{}{
			"store":            "true",
			"reasoning_effort": "high",
		})
		req := Request{
			Messages: []Message{{Role: "user", Content: "hi"}},
			RawBody:  rawBody,
		}
		body, err := buildRequestBody(provider, "llama3", req)
		if err != nil {
			t.Fatalf("buildRequestBody() error = %v", err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Failed to unmarshal body: %v", err)
		}
		if _, ok := payload["reasoning_effort"]; ok {
			t.Error("reasoning_effort should have been removed for groq")
		}
		if _, ok := payload["store"]; ok {
			t.Error("store should have been removed for groq")
		}
	})

	t.Run("native ollama returns ollama body", func(t *testing.T) {
		provider := config.Provider{Name: "ollama", BaseURL: "http://localhost:11434"}
		req := Request{
			Messages: []Message{{Role: "user", Content: "hello"}},
		}
		body, err := buildRequestBody(provider, "llama3", req)
		if err != nil {
			t.Fatalf("buildRequestBody() error = %v", err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Failed to unmarshal body: %v", err)
		}
		if payload["model"] != "llama3" {
			t.Errorf("Expected model 'llama3', got %v", payload["model"])
		}
		// Ollama body uses a 'stream' field at root, distinguishing it from OpenAI-format bodies
		if _, ok := payload["stream"]; !ok {
			t.Error("Expected 'stream' field in ollama body")
		}
	})
}

func TestFindModelInProvider(t *testing.T) {
	cfg := config.Config{
		Providers: []config.Provider{
			{
				Name: "provider1",
				Models: []config.Model{
					{Name: "model-a"},
					{Name: "model-b"},
				},
			},
			{
				Name: "provider2",
				Models: []config.Model{
					{Name: "model-c"},
				},
			},
		},
	}

	t.Run("found", func(t *testing.T) {
		p, m, err := findModelInProvider(nil, cfg, "provider1", "model-b") //nolint:staticcheck
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name != "provider1" {
			t.Errorf("Expected provider 'provider1', got '%s'", p.Name)
		}
		if m != "model-b" {
			t.Errorf("Expected model 'model-b', got '%s'", m)
		}
	})

	t.Run("provider not found", func(t *testing.T) {
		_, _, err := findModelInProvider(nil, cfg, "unknown-provider", "model-a") //nolint:staticcheck
		if err == nil {
			t.Error("Expected error for unknown provider")
		}
	})

	t.Run("model not found in provider", func(t *testing.T) {
		_, _, err := findModelInProvider(nil, cfg, "provider1", "model-z") //nolint:staticcheck
		if err == nil {
			t.Error("Expected error for unknown model")
		}
	})
}

func TestFindModelAnywhere(t *testing.T) {
	cfg := config.Config{
		Providers: []config.Provider{
			{
				Name: "provider1",
				Models: []config.Model{
					{Name: "model-a"},
				},
			},
			{
				Name: "provider2",
				Models: []config.Model{
					{Name: "model-b"},
				},
			},
		},
	}
	config.LoadTestConfig(cfg)
	defer config.ResetTestConfig()

	t.Run("found in first provider", func(t *testing.T) {
		p, m, err := findModelAnywhere(nil, "model-a") //nolint:staticcheck
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name != "provider1" {
			t.Errorf("Expected provider 'provider1', got '%s'", p.Name)
		}
		if m != "model-a" {
			t.Errorf("Expected model 'model-a', got '%s'", m)
		}
	})

	t.Run("found in second provider", func(t *testing.T) {
		p, m, err := findModelAnywhere(nil, "model-b") //nolint:staticcheck
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name != "provider2" {
			t.Errorf("Expected provider 'provider2', got '%s'", p.Name)
		}
		if m != "model-b" {
			t.Errorf("Expected model 'model-b', got '%s'", m)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, _, err := findModelAnywhere(nil, "unknown-model") //nolint:staticcheck
		if err == nil {
			t.Error("Expected error for unknown model")
		}
	})
}
