package control_test

import (
	"io"
	"testing"
	"time"
)

const closeDeadline = 5 * time.Second

func closeBounded(tb testing.TB, closer io.Closer) error {
	tb.Helper()
	done := make(chan error, 1)
	go func() { done <- closer.Close() }()
	select {
	case err := <-done:
		return err
	case <-time.After(closeDeadline):
		tb.Fatalf("%T.Close did not return within %s", closer, closeDeadline)
		return nil
	}
}
