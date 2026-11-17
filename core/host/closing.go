package host

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync/atomic"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func closeRead(closer io.Closer) {
	logCleanup(closer.Close())
}

func logCleanup(err error) {
	if err != nil {
		slog.Debug("cleanup failed", "err", err)
	}
}

func logWriteFailure(_ int, err error) {
	logCleanup(err)
}

type boundedLog struct {
	busy      atomic.Bool
	abandoned atomic.Int64
}

func (l *boundedLog) warn(line func()) {
	if !l.busy.CompareAndSwap(false, true) {
		l.abandoned.Add(1)
		return
	}
	budget, stop := writeBudget()
	defer stop()
	logged := make(chan struct{})
	go func() {
		defer close(logged)
		line()
		l.announceAbandoned()
	}()
	select {
	case <-logged:
	case <-budget:
	}
}

func (l *boundedLog) announceAbandoned() {
	for {
		if n := l.abandoned.Swap(0); n > 0 {
			slog.Warn("warning lines were abandoned while the log was blocked", "abandoned", n)
		}
		l.busy.Store(false)
		if l.abandoned.Load() == 0 || !l.busy.CompareAndSwap(false, true) {
			return
		}
	}
}

func warningWriter(writer io.Writer) io.Writer {
	if writer == nil {
		return os.Stderr
	}
	return writer
}

func warnTo(w io.Writer, lang i18n.Language, key string, params map[string]any) {
	logWriteFailure(fmt.Fprintln(warningWriter(w), newHostMessages().text(lang, key, params)))
}

func warnLive(w io.Writer, lang languageSource, key string, params map[string]any) {
	warnTo(w, lang(), key, params)
}
