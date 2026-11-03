package host

import (
	"os"
	"time"
)

const (
	maxHookInput            = 1 << 20
	commandDialTimeout      = 500 * time.Millisecond
	ownerOnlyFileMode       = os.FileMode(0o600)
	daemonEventBuffer       = 256
	daemonLineBufferInitial = 64 * 1024
	daemonLineBufferMax     = 8 * 1024 * 1024
	trailerFields           = 2
	pidReadBytes            = 32
	pingInterval            = 30 * time.Second
	statusInterval          = 60 * time.Second
	unameFieldCount         = 2
	backoffFactor           = 2
	bridgeEventBuffer       = 8
	turnQueueCapacity       = 64
	pollBatchSize           = 100
	retryJitter             = 0.2
	privateDirMode          = os.FileMode(0o750)
	healthReadHeaderTimeout = 5 * time.Second
)
