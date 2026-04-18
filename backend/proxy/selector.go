package proxy

import (
	"ai-proxy/config"
	"sort"
)

// getAllCandidates returns all provider:model combinations in tiered order:
// Tier 1: StatusActive (Round-robin within same priority)
// Tier 2: StatusDegraded (Sorted by EWMA latency)
// Tier 3: StatusBlockedTemp (Sorted by EWMA latency)
// Skip: StatusBlockedFatal
func getAllCandidates(cfg config.Config) []ProviderModelPair {
	providers := sortedProviders(cfg.Providers)
	maxModelsCount := findMaxModels(providers)

	lastProvider, lastModelName, _ := GetLastSuccessfulModel()

	var activeCandidates []ProviderModelPair
	var degradedCandidates []ProviderModelPair
	var blockedCandidates []ProviderModelPair

	for round := 0; round < maxModelsCount; round++ {
		for _, p := range providers {
			if round >= len(p.Models) {
				continue
			}

			modelName := p.Models[round].Name
			status, _ := GetModelStatus(p.Name, modelName)

			candidate := ProviderModelPair{
				Provider: p,
				Model:    modelName,
			}

			switch status {
			case StatusActive:
				activeCandidates = append(activeCandidates, candidate)
			case StatusDegraded:
				degradedCandidates = append(degradedCandidates, candidate)
			case StatusBlockedTemp:
				blockedCandidates = append(blockedCandidates, candidate)
			}
		}
	}

	// Sort Degraded and Blocked by EWMA
	sort.Slice(degradedCandidates, func(i, j int) bool {
		return GetModelLatency(degradedCandidates[i].Provider.Name, degradedCandidates[i].Model) <
			GetModelLatency(degradedCandidates[j].Provider.Name, degradedCandidates[j].Model)
	})
	sort.Slice(blockedCandidates, func(i, j int) bool {
		return GetModelLatency(blockedCandidates[i].Provider.Name, blockedCandidates[i].Model) <
			GetModelLatency(blockedCandidates[j].Provider.Name, blockedCandidates[j].Model)
	})

	// Final assembly: Active (with last model priority) -> Degraded -> Blocked
	finalCandidates := make([]ProviderModelPair, 0, len(activeCandidates)+len(degradedCandidates)+len(blockedCandidates))
	var lastModelCandidate *ProviderModelPair

	for i, c := range activeCandidates {
		if c.Provider.Name == lastProvider && c.Model == lastModelName {
			lastModelCandidate = &activeCandidates[i]
			continue
		}
		finalCandidates = append(finalCandidates, c)
	}

	if lastModelCandidate != nil {
		finalCandidates = append([]ProviderModelPair{*lastModelCandidate}, finalCandidates...)
	}

	finalCandidates = append(finalCandidates, degradedCandidates...)
	finalCandidates = append(finalCandidates, blockedCandidates...)

	return finalCandidates
}

// sortedProviders returns providers sorted by priority (filters out disabled)
func sortedProviders(providers []config.Provider) []config.Provider {
	var enabled []config.Provider
	for _, p := range providers {
		if p.IsEnabled() {
			enabled = append(enabled, p)
		}
	}

	sorted := make([]config.Provider, len(enabled))
	copy(sorted, enabled)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	return sorted
}

// findMaxModels returns the maximum number of models across all providers
func findMaxModels(providers []config.Provider) int {
	maxModels := 0
	for _, p := range providers {
		if len(p.Models) > maxModels {
			maxModels = len(p.Models)
		}
	}
	return maxModels
}
