// Package boundedbuf collects the head of a command's output and drains the rest.
package boundedbuf

import "unicode/utf8"

// Buffer keeps at most limit bytes of what is written to it and accepts every
// write in full, so a child process never blocks on a full pipe.
type Buffer struct {
	limit     int
	data      []byte
	truncated bool
}

// New returns a Buffer that keeps at most limit bytes.
func New(limit int) *Buffer {
	return &Buffer{limit: limit}
}

// Write keeps what fits under the limit and records that the rest was dropped.
func (b *Buffer) Write(data []byte) (int, error) {
	written := len(data)
	if room := b.limit - len(b.data); room > 0 {
		keep := min(room, len(data))
		b.data = append(b.data, data[:keep]...)
		data = data[keep:]
	}
	if len(data) > 0 {
		b.truncated = true
	}
	return written, nil
}

// String returns the kept bytes.
func (b *Buffer) String() string {
	data := b.data
	if b.truncated {
		data = withoutPartialRune(data)
	}
	return string(data)
}

func withoutPartialRune(data []byte) []byte {
	for start := len(data) - 1; start >= 0 && start >= len(data)-utf8.UTFMax; start-- {
		if utf8.RuneStart(data[start]) {
			if !utf8.FullRune(data[start:]) {
				return data[:start]
			}
			return data
		}
	}
	return data
}

// Truncated reports whether some written bytes were dropped.
func (b *Buffer) Truncated() bool {
	return b.truncated
}
