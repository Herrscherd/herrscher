package host

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"sync"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func closeRead(closer io.Closer) {
	logCleanup(closer.Close())
}

func logCleanup(err error) {
	logCleanupWith(slog.Default(), err)
}

func logCleanupWith(logger *slog.Logger, err error) {
	if err != nil {
		logger.Debug("cleanup failed", "err", err)
	}
}

func logWriteFailure(_ int, err error) {
	logCleanup(err)
}

type bilanSum struct {
	lost        int
	unconfirmed int
}

type namedLine struct {
	key  string
	line func(logger *slog.Logger)
}

type boundedLog struct {
	mu         sync.Mutex
	logger     *slog.Logger
	notes      []namedLine
	bilan      bilanSum
	writeBilan func(logger *slog.Logger, lost, unconfirmed int)
	running    bool
	idle       []chan struct{}
}

func (l *boundedLog) logWriteFailure(_ int, err error) {
	logCleanupWith(l.logger, err)
}

func (l *boundedLog) addBilan(lost, unconfirmed int, write func(logger *slog.Logger, lost, unconfirmed int)) {
	if lost == 0 && unconfirmed == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bilan.lost += lost
	l.bilan.unconfirmed += unconfirmed
	l.writeBilan = write
	l.startLocked()
}

func (l *boundedLog) reserve(key string, line func(logger *slog.Logger)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if index := slices.IndexFunc(l.notes, func(note namedLine) bool { return note.key == key }); index >= 0 {
		l.notes[index].line = line
	} else {
		l.notes = append(l.notes, namedLine{key: key, line: line})
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
			l.running = false
			l.wakeIdleLocked()
			l.mu.Unlock()
			return
		}
		l.mu.Unlock()
		line(l.logger)
	}
}

func (l *boundedLog) takeLocked() func(logger *slog.Logger) {
	if l.bilan.lost != 0 || l.bilan.unconfirmed != 0 {
		lost, unconfirmed, write := l.bilan.lost, l.bilan.unconfirmed, l.writeBilan
		l.bilan = bilanSum{}
		return func(logger *slog.Logger) { write(logger, lost, unconfirmed) }
	}
	if len(l.notes) == 0 {
		return nil
	}
	next := l.notes[0]
	l.notes = l.notes[1:]
	return next.line
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

func warnTo(messages *hostMessages, w io.Writer, lang i18n.Language, key string, params map[string]any) {
	logWriteFailure(fmt.Fprintln(warningWriter(w), messages.text(lang, key, params)))
}

func warnLive(messages *hostMessages, w io.Writer, lang languageSource, key string, params map[string]any) {
	warnTo(messages, w, lang(), key, params)
}
