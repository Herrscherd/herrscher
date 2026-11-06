package clitext

// SentinelError is an error whose message is an English sentence. Operators read
// the text rendered from the catalog through the key passed to failure or
// localize; Error keeps a readable sentence for developers and logs.
type SentinelError string

func (s SentinelError) Error() string {
	return string(s)
}
