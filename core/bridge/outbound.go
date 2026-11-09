package bridge

import (
	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/outbox"
)

type outboundSettings struct {
	root     string
	session  string
	maxBytes int64
}

type turnOutbox struct {
	producer contracts.FileProducer
	settings outboundSettings
	dir      string
}

func beginTurnOutbox(backend contracts.Backend, settings outboundSettings) turnOutbox {
	producer, ok := fileProducerOf(backend)
	if !ok || settings.root == "" {
		return turnOutbox{}
	}
	dir, err := outbox.NewTurnDir(settings.root, settings.session)
	if err != nil {
		logger.Warn("turn outbox unavailable, this turn sends no files", "err", err)
		return turnOutbox{}
	}
	return turnOutbox{producer: producer, settings: settings, dir: dir}
}

func (t *turnOutbox) emitFiles(sink contracts.EventSink) {
	if t.dir == "" {
		return
	}
	files, refused := outbox.Vet(t.dir, t.producer.TakeFiles(), t.settings.maxBytes)
	names := make([]string, 0, len(refused))
	for _, refusal := range refused {
		logger.Warn("outbound file refused, not sent", "file", refusal.Name, "err", refusal.Cause)
		names = append(names, refusal.Name)
	}
	if len(files) == 0 && len(names) == 0 {
		t.discard()
		return
	}
	value, err := outbox.Encode(outbox.Frame{Dir: t.dir, Files: files, Refused: names})
	if err != nil {
		logger.Warn("outbound frame not encoded, files dropped", "err", err)
		t.discard()
		return
	}
	t.dir = ""
	sink.Emit(contracts.Event{T: outbox.EventFiles, Value: value})
}

func (t *turnOutbox) discard() {
	if t.dir == "" {
		return
	}
	if err := outbox.Remove(t.settings.root, t.dir); err != nil {
		logger.Warn("turn outbox not removed", "err", err)
	}
	t.dir = ""
}
