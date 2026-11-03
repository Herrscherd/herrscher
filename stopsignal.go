package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
)

const (
	exitInterrupted = 130
	exitTerminated  = 143
)

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

func stoppedBy(ctx context.Context, err error) (os.Signal, bool) {
	stop, ok := errors.AsType[stopSignalError](context.Cause(ctx))
	if !ok || !errors.Is(err, context.Canceled) {
		return nil, false
	}
	return stop.sig, true
}

func reportOutcome(ctx context.Context, err error) int {
	sig, stopped := stoppedBy(ctx, err)
	switch {
	case !stopped:
		return reportError(err)
	case sig == syscall.SIGTERM:
		return exitTerminated
	default:
		return exitInterrupted
	}
}

func reportDaemonOutcome(ctx context.Context, err error) int {
	if sig, stopped := stoppedBy(ctx, err); stopped && sig == syscall.SIGTERM {
		return 0
	}
	return reportOutcome(ctx, err)
}

func outcomeReporterFor(cmd string) func(context.Context, error) int {
	switch cmd {
	case "serve", "bridge":
		return reportDaemonOutcome
	default:
		return reportOutcome
	}
}
