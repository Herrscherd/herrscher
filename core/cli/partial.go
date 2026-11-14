package cli

import "errors"

// PartialError is a failure that still produced output. PartialOutput is the text
// the command produced before it failed, empty when it produced none.
type PartialError interface {
	error
	PartialOutput() string
}

// PartialOutputOf returns the output that err carries from a failed command, or the
// empty string when err carries none, even when it is wrapped.
func PartialOutputOf(err error) string {
	partial, ok := errors.AsType[PartialError](err)
	if !ok {
		return ""
	}
	return partial.PartialOutput()
}
