package cli

// PartialError is a failure that still produced output. PartialOutput is the text
// the command produced before it failed, empty when it produced none.
type PartialError interface {
	error
	PartialOutput() string
}
