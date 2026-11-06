package main

import (
	"errors"
	"fmt"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
)

type reason uint8

const (
	reasonUnknown reason = iota
	programmerError
	technicalWrap
	keyedSentinel
)

var (
	errUnknownReason = errors.New("unknown exemption reason")
	errWrapNotFormat = errors.New("a technical wrap ends with the wrapped error")
)

func (why reason) known() bool {
	switch why {
	case programmerError, technicalWrap, keyedSentinel:
		return true
	case reasonUnknown:
		return false
	}
	return false
}

type exemption struct {
	directory string
	file      string
	text      string
	why       reason
}

const (
	cliDirectory    = "core/cli"
	cliRegistry     = "registry.go"
	tuiDirectory    = "plugins/terminal/tui"
	imageDecodeFile = "image_decode.go"
	imageFetchFile  = "image_fetch.go"
)

func exemptions() []exemption {
	return []exemption{
		{directory: cliDirectory, file: cliRegistry, text: "cli: command with empty path", why: programmerError},
		{directory: cliDirectory, file: cliRegistry, text: "cli: duplicate command", why: programmerError},
		{directory: cliDirectory, file: cliRegistry, text: "command %q: %w", why: programmerError},
		{directory: cliDirectory, file: cliRegistry, text: "param %q: %w", why: programmerError},
		{directory: tuiDirectory, file: imageDecodeFile, text: "read image header: %w", why: technicalWrap},
		{directory: tuiDirectory, file: imageDecodeFile, text: "decode image: %w", why: technicalWrap},
		{directory: tuiDirectory, file: imageDecodeFile, text: "encode png: %w", why: technicalWrap},
		{directory: tuiDirectory, file: imageFetchFile, text: "parse image url: %w", why: technicalWrap},
		{directory: tuiDirectory, file: imageFetchFile, text: "image url scheme %q %w", why: technicalWrap},
		{directory: tuiDirectory, file: imageFetchFile, text: "image url %q %w", why: technicalWrap},
		{directory: tuiDirectory, file: imageFetchFile, text: "image host %s %w", why: technicalWrap},
		{directory: tuiDirectory, file: imageFetchFile, text: "fetch %s: %w", why: technicalWrap},
	}
}

func validateExemptions(list []exemption) error {
	for _, entry := range list {
		if !entry.why.known() {
			return fmt.Errorf("exemption %q: %w", entry.text, errUnknownReason)
		}
		if entry.why == technicalWrap && !strings.HasSuffix(entry.text, "%w") {
			return fmt.Errorf("exemption %q: %w", entry.text, errWrapNotFormat)
		}
	}
	return nil
}

func isExempt(position token.Position, text string) bool {
	slashed := filepath.ToSlash(position.Filename)
	return slices.ContainsFunc(exemptions(), func(entry exemption) bool {
		suffix := entry.directory + "/" + entry.file
		return entry.text == text && (slashed == suffix || strings.HasSuffix(slashed, "/"+suffix))
	})
}
