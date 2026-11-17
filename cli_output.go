package main

import (
	"fmt"
	"io"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

func writeCLIText(w io.Writer, text string) error {
	_, err := fmt.Fprintln(w, termpaint.EscapeLines(text).String())
	return err
}

func cliErrorLine(line string) string {
	return termpaint.Escape(line).String()
}

func escapedParam(name, value string) clitext.Params {
	return clitext.Params{name: termpaint.Escape(value).String()}
}
