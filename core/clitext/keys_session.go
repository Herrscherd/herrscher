package clitext

const (
	AttachNotAvailable Key = "cli.attach.not_available"
	AttachNoSession    Key = "cli.attach.no_session"
)

var sessionKeys = map[Key][]string{
	AttachNotAvailable: {"Verb"},
	AttachNoSession:    nil,
}
