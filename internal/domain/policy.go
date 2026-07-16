package domain

import "strings"

// Default-detail image inputs use Codex's resized-image estimate, rounded up.
const EstimatedImageInputTokens = 2_000

func EstimateTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}

	return EstimateByteTokens(len(trimmed))
}

func EstimateByteTokens(size int) int {
	if size <= 0 {
		return 0
	}
	return (size + 3) / 4
}
