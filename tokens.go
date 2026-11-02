package main

import (
	"time"

	"github.com/Herrscherd/herrscher/plugins/terminal"
)

const (
	argJSON            = "--json"
	argName            = "--name"
	verbSession        = "session"
	cmdInstall         = "install"
	cmdStatus          = "status"
	cmdUpdate          = "update"
	kindTerminal       = terminal.Kind
	exitUsage          = 2
	minArgsWithCommand = 2
	quotedMinLen       = 2
	envLineInitial     = 64 * 1024
	envLineLimit       = 1 << 20
	modeSecret         = 0o600
	attachBuffer       = 256
	attachTimeout      = 30 * time.Second
	announceInterval   = 2 * time.Second
)
