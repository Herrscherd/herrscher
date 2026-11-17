package main

import (
	"fmt"
	"io"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

func printCLIText(w io.Writer, text string) error {
	_, err := fmt.Fprintln(w, termpaint.EscapeLines(text).String())
	return err
}

func cliErrorLine(line string) string {
	return termpaint.Escape(line).String()
}
