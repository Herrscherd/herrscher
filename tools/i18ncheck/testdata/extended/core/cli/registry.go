package cli

import "errors"

var (
	errEmptyPath        = errors.New("cli: command with empty path")
	errDuplicateCommand = errors.New("cli: duplicate command")
	errUnknownCommand   = errors.New("unknown command")
	errFlagNeedsValue   = errors.New("needs a value")
	errNewProgrammer    = errors.New("cli: programmer message outside the list")
)
