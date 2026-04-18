package proxy

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-proxy/config"
)

func TestDecodeResponse_OpenAI_Usage(t *testing.T) {
	provider := config.Provider{Name: "openai", BaseURL: "https://api.openai.com"}
	jsonResp := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-3.5-turbo-0613",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello there!"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 9,
			"completion_tokens": 12,
			"total_tokens": 21
		}
	}`

	body := bytes.NewReader([]byte(jsonResp))
	resp, err := decodeResponse(provider, body)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Usage == nil {
		t.Fatal("Expected usage to be not nil")
	}

	if resp.Usage.PromptTokens != 9 {
		t.Errorf("Expected prompt_tokens 9, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 12 {
		t.Errorf("Expected completion_tokens 12, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 21 {
		t.Errorf("Expected total_tokens 21, got %d", resp.Usage.TotalTokens)
	}
}

func TestDecodeOllamaResponse_Usage(t *testing.T) {
	provider := config.Provider{Name: "ollama", BaseURL: "http://localhost:11434"}
	jsonResp := `{
		"model": "llama3",
		"created_at": "2024-04-12T12:00:00Z",
		"message": {
			"role": "assistant",
			"content": "Hello from Ollama!"
		},
		"done": true,
		"prompt_eval_count": 15,
		"eval_count": 20
	}`

	body := bytes.NewReader([]byte(jsonResp))
	resp, err := decodeResponse(provider, body)
	if err != nil {
		t.Fatalf("Failed to decode Ollama response: %v", err)
	}

	if resp.Usage == nil {
		t.Fatal("Expected usage to be not nil")
	}

	if resp.Usage.PromptTokens != 15 {
		t.Errorf("Expected prompt_tokens 15, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 20 {
		t.Errorf("Expected completion_tokens 20, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 35 {
		t.Errorf("Expected total_tokens 35, got %d", resp.Usage.TotalTokens)
	}

	if len(resp.Choices) == 0 || resp.Choices[0].FinishReason != "stop" {
		t.Errorf("Expected finish_reason 'stop', got %v", resp.Choices[0].FinishReason)
	}
}

func TestStreamOllamaResponse_Usage(t *testing.T) {
	jsonResp := `{"model": "llama3", "message": {"role": "assistant", "content": "Hello"}, "done": false}
{"model": "llama3", "message": {"role": "assistant", "content": " world"}, "done": true, "prompt_eval_count": 10, "eval_count": 5}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprint(w, jsonResp)
	}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to create mock server request: %v", err)
	}
	defer resp.Body.Close()

	w := httptest.NewRecorder()
	
	err = streamOllamaResponse(resp, w, &mockFlusher{})
	if err != nil {
		t.Fatalf("streamOllamaResponse failed: %v", err)
	}

	output := w.Body.String()
	// Should contain usage in the last data block
	expectedUsage := `"usage":{"completion_tokens":5,"prompt_tokens":10,"total_tokens":15}`
	if !bytes.Contains([]byte(output), []byte(expectedUsage)) {
		t.Errorf("Expected output to contain usage info. Output: %s", output)
	}
	
	expectedFinishReason := `"finish_reason":"stop"`
	if !bytes.Contains([]byte(output), []byte(expectedFinishReason)) {
		t.Errorf("Expected output to contain finish_reason info. Output: %s", output)
	}
}

type mockFlusher struct{}
func (m *mockFlusher) Flush() {}
