package bridge

import "time"

const (
	dirPrivate    = 0o700
	dirShared     = 0o750
	fileSecret    = 0o600
	maxRedirects  = 10
	dialTimeout   = 30 * time.Second
	dialKeepAlive = 30 * time.Second
	partsCap      = 3
)
