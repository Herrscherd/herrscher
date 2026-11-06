package main

import (
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
		if isWord(strings.Trim(field, wordEdges)) {
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
	return strings.HasPrefix(fields[0], "-") || strings.HasPrefix(fields[0], "/") || strings.HasPrefix(fields[0], "<")
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
