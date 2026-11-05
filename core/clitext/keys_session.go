package clitext

// Keys of the messages of attached sessions.
const (
	AttachNotAvailable = "cli.attach.not_available"
	AttachNoSession    = "cli.attach.no_session"
)

func sessionKeys() map[Key][]string {
	return map[Key][]string{
		AttachNotAvailable: {ParamVerb},
		AttachNoSession:    nil,
	}
}
