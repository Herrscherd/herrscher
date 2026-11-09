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

func emitTurnFiles(sink contracts.EventSink, turn turnOutbox) {
	if turn.dir == "" {
		return
	}
	files, refused := outbox.Vet(turn.dir, turn.producer.TakeFiles(), turn.settings.maxBytes)
	names := make([]string, 0, len(refused))
	for _, refusal := range refused {
		logger.Warn("outbound file refused, not sent", "file", refusal.Name, "err", refusal.Cause)
		names = append(names, refusal.Name)
	}
	if len(files) == 0 && len(names) == 0 {
		removeTurnOutbox(turn.settings.root, turn.dir)
		return
	}
	value, err := outbox.Encode(outbox.Frame{Dir: turn.dir, Files: files, Refused: names})
	if err != nil {
		logger.Warn("outbound frame not encoded, files dropped", "err", err)
		removeTurnOutbox(turn.settings.root, turn.dir)
		return
	}
	sink.Emit(contracts.Event{T: outbox.EventFiles, Value: value})
}

func removeTurnOutbox(root, dir string) {
	if err := outbox.Remove(root, dir); err != nil {
		logger.Warn("turn outbox not removed", "err", err)
	}
}
