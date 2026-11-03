package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
)

const exitInterrupted = 130

type stopSignalError struct {
	sig os.Signal
}

func (s stopSignalError) Error() string {
	return "stopped by " + s.sig.String()
}

func notifyStop(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	arrivals := make(chan os.Signal, 1)
	signal.Notify(arrivals, os.Interrupt, syscall.SIGTERM)
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		select {
		case sig := <-arrivals:
			cancel(stopSignalError{sig: sig})
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		signal.Stop(arrivals)
		cancel(nil)
		<-watched
	}
}

func reportOutcome(ctx context.Context, err error) int {
	if stop, ok := errors.AsType[stopSignalError](context.Cause(ctx)); ok && errors.Is(err, context.Canceled) {
		if stop.sig == syscall.SIGTERM {
			return 0
		}
		return exitInterrupted
	}
	return reportError(err)
}
