package tui

import "strings"

func fuzzyFilter[T any](items []T, query string, text func(T) string) []T {
	needle := strings.ToLower(strings.TrimSpace(query))
	out := make([]T, 0, len(items))
	for _, item := range items {
		if fuzzySubsequence(needle, text(item)) {
			out = append(out, item)
		}
	}
	return out
}

func fuzzySubsequence(needle, haystack string) bool {
	runes := []rune(strings.ToLower(haystack))
	pos := 0
	for _, want := range needle {
		found := -1
		for index := pos; index < len(runes); index++ {
			if runes[index] == want {
				found = index
				break
			}
		}
		if found < 0 {
			return false
		}
		pos = found + 1
	}
	return true
}
