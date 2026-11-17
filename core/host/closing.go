package host

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

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

const boundedLogQueue = 8

type boundedLog struct {
	mu        sync.Mutex
	queue     []func()
	summary   func()
	abandoned int
	running   bool
	idle      []chan struct{}
}

func (l *boundedLog) warn(line func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.queue) >= boundedLogQueue {
		l.abandoned++
		return
	}
	l.queue = append(l.queue, line)
	l.startLocked()
}

func (l *boundedLog) reserve(line func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if previous := l.summary; previous != nil {
		l.summary = func() {
			previous()
			line()
		}
	} else {
		l.summary = line
	}
	l.startLocked()
}

func (l *boundedLog) drain(budget <-chan struct{}) bool {
	l.mu.Lock()
	if !l.running {
		l.mu.Unlock()
		return true
	}
	idle := make(chan struct{})
	l.idle = append(l.idle, idle)
	l.mu.Unlock()
	select {
	case <-idle:
		return true
	case <-budget:
		return false
	}
}

func (l *boundedLog) startLocked() {
	if !l.running {
		l.running = true
		go l.run()
	}
}

func (l *boundedLog) run() {
	for {
		l.mu.Lock()
		line := l.takeLocked()
		if line == nil {
			lost := l.abandoned
			l.abandoned = 0
			if lost == 0 {
				l.running = false
				l.wakeIdleLocked()
				l.mu.Unlock()
				return
			}
			l.mu.Unlock()
			slog.Warn("warning lines were abandoned while the log was blocked", "abandoned", lost)
			continue
		}
		l.mu.Unlock()
		line()
	}
}

func (l *boundedLog) takeLocked() func() {
	if line := l.summary; line != nil {
		l.summary = nil
		return line
	}
	if len(l.queue) == 0 {
		return nil
	}
	line := l.queue[0]
	l.queue = l.queue[1:]
	return line
}

func (l *boundedLog) wakeIdleLocked() {
	for _, idle := range l.idle {
		close(idle)
	}
	l.idle = nil
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
