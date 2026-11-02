package errwriter

import (
	"fmt"
	"io"
)

type Writer struct {
	w   io.Writer
	err error
}

func New(w io.Writer) *Writer {
	return &Writer{w: w}
}

func (w *Writer) Println(a ...any) {
	if w.err != nil {
		return
	}
	_, w.err = fmt.Fprintln(w.w, a...)
}

func (w *Writer) Printf(format string, a ...any) {
	if w.err != nil {
		return
	}
	_, w.err = fmt.Fprintf(w.w, format, a...)
}

func (w *Writer) Err() error {
	return w.err
}

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

func (w *Writer) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.w.Write(p)
	w.err = err
	return n, err
}
