package main

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	minWordRunes = 2
	wordEdges    = ".,:;!?()[]{}'\"`"
)

func isProse(text string) bool {
	if isCommandSyntax(text) {
		return false
	}
	for _, field := range strings.Fields(text) {
		if isWordField(field) {
			return true
		}
	}
	return false
}

func isCommandSyntax(text string) bool {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return true
	}
	if !hasCommandPrefix(fields[0]) {
		return false
	}
	return !slices.ContainsFunc(fields[1:], isWordField)
}

func hasCommandPrefix(token string) bool {
	return strings.HasPrefix(token, "-") || strings.HasPrefix(token, "/") || strings.HasPrefix(token, "<")
}

func isWordField(field string) bool {
	return isWord(strings.Trim(field, wordEdges))
}

func isWord(word string) bool {
	if utf8.RuneCountInString(word) < minWordRunes {
		return false
	}
	for _, letter := range word {
		if !unicode.IsLetter(letter) {
			return false
		}
	}
	return true
}
