package bridge

import (
	"io"
	"log/slog"
)

func closeQuietly(c io.Closer) {
	if err := c.Close(); err != nil {
		slog.Warn("close", "err", err)
	}
}
