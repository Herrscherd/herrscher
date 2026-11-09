package usage

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
)

const (
	transcriptBuffer   = 1 << 20
	maxTranscriptLine  = 32 << 20
	linesBetweenChecks = 4096
)

func scanTranscript(ctx context.Context, reader io.Reader, visit func(line []byte)) (int64, error) {
	buffered := bufio.NewReaderSize(reader, transcriptBuffer)
	var consumed int64
	var line longLine
	for lines := 1; ; lines++ {
		chunk, err := buffered.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			line.grow(chunk)
			continue
		}
		if errors.Is(err, io.EOF) {
			return consumed, nil
		}
		if err != nil {
			return consumed, fmt.Errorf("usage transcript: %w", err)
		}
		consumed += line.end(chunk, visit)
		if lines%linesBetweenChecks == 0 && ctx.Err() != nil {
			return consumed, ctx.Err()
		}
	}
}

type longLine struct {
	pending  []byte
	partial  int64
	overflow bool
}

func (l *longLine) grow(chunk []byte) {
	l.partial += int64(len(chunk))
	l.overflow = l.overflow || len(l.pending)+len(chunk) > maxTranscriptLine
	if !l.overflow {
		l.pending = append(l.pending, chunk...)
	}
}

func (l *longLine) end(chunk []byte, visit func(line []byte)) int64 {
	if !l.overflow {
		visit(joinedLine(l.pending, chunk))
	}
	size := l.partial + int64(len(chunk))
	l.pending, l.partial, l.overflow = l.pending[:0], 0, false
	return size
}

func joinedLine(pending, chunk []byte) []byte {
	if len(pending) == 0 {
		return chunk
	}
	return append(pending, chunk...)
}
