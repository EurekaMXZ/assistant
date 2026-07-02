package domain

import "strings"

func EstimateTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}

	runes := len([]rune(trimmed))
	if runes < 8 {
		return 8
	}

	return (runes / 2) + 8
}
