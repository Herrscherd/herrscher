// Package errwriter keeps the first write error of a stream so callers check it once.
package errwriter

import (
	"fmt"
	"io"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

// Writer records the first error of the writes made through it and skips the rest.
type Writer struct {
	w   io.Writer
	err error
}

// New wraps w in a Writer.
func New(w io.Writer) *Writer {
	return &Writer{w: w}
}

// Println writes the operands like fmt.Println, escaped for the terminal, unless an earlier write failed.
func (w *Writer) Println(args ...any) {
	w.writeEscaped(fmt.Sprintln(args...))
}

// Printf writes like fmt.Printf, escaped for the terminal, unless an earlier write failed.
func (w *Writer) Printf(format string, args ...any) {
	w.writeEscaped(fmt.Sprintf(format, args...))
}

func (w *Writer) writeEscaped(text string) {
	if w.err != nil {
		return
	}
	_, w.err = io.WriteString(w.w, termpaint.EscapeLines(text).String())
}

// Err returns the first write error, or nil.
func (w *Writer) Err() error {
	return w.err
}

// Finish returns the exit status for code after checking both streams for write errors.
func Finish(code int, out, errOut *Writer) int {
	if out.err != nil {
		errOut.Printf("write stdout: %v\n", out.err)
		if code == 0 {
			return 1
		}
	}
	if errOut.err != nil && code == 0 {
		return 1
	}
	return code
}

func (w *Writer) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	written, err := w.w.Write(data)
	w.err = err
	return written, err
}
