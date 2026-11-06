package main

import (
	"go/token"
	"path/filepath"
	"slices"
	"strings"
)

type programmerError struct {
	directory string
	file      string
	text      string
}

const (
	cliDirectory = "core/cli"
	cliRegistry  = "registry.go"
)

func programmerErrors() []programmerError {
	return []programmerError{
		{directory: cliDirectory, file: cliRegistry, text: "cli: command with empty path"},
		{directory: cliDirectory, file: cliRegistry, text: "cli: duplicate command"},
	}
}

func isProgrammerError(position token.Position, text string) bool {
	slashed := filepath.ToSlash(position.Filename)
	return slices.ContainsFunc(programmerErrors(), func(entry programmerError) bool {
		return entry.text == text && strings.HasSuffix(slashed, entry.directory+"/"+entry.file)
	})
}
