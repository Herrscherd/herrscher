package bridge

import "io"

func closeAndWarn(c io.Closer) {
	if err := c.Close(); err != nil {
		logger.Warn("close failed", "err", err)
	}
}
