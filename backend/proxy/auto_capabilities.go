package proxy

import "ai-proxy/config"

// AutoModelCapabilitiesDescription documents how the synthetic auto model handles
// reasoning params across OpenAI /api/v1/models and MCP models/list.
const AutoModelCapabilitiesDescription = "Automatically selects the best available provider and model based on performance and availability. Accepts reasoning_effort / reasoning and maps or strips them per candidate; does not guarantee a reasoning-capable upstream. On OpenCode failover, only effort is preserved (OpenRouter sibling keys such as max_tokens/exclude are dropped)."

// AnyEnabledModelSupportsReasoning reports whether any enabled provider has a
// model with Reasoning: true. Used for the synthetic auto capability flag so
// LiteLLM-style clients send effort only when a reasoning candidate may exist.
func AnyEnabledModelSupportsReasoning(cfg config.Config) bool {
	for _, p := range cfg.Providers {
		if !p.IsEnabled() {
			continue
		}
		for _, m := range p.Models {
			if m.Reasoning {
				return true
			}
		}
	}
	return false
}

// AutoModelCapabilities builds the shared capability map for the synthetic auto model.
func AutoModelCapabilities(reasoning bool) map[string]interface{} {
	return map[string]interface{}{
		"description": AutoModelCapabilitiesDescription,
		"reasoning":   reasoning,
	}
}
