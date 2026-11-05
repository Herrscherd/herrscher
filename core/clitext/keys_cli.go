package clitext

// Keys of the messages the command line prints itself.
const (
	TaskNeedsText               = "cli.task.needs_text"
	ErrorLine                   = "cli.error.line"
	CommandUnknown              = "cli.command.unknown"
	WhoamiUnknownArgument       = "cli.whoami.unknown_argument"
	EnvContinuing               = "cli.env.continuing"
	EnvOpenFailed               = "cli.env.open_failed"
	EnvSetFailed                = "cli.env.set_failed"
	EnvReadFailed               = "cli.env.read_failed"
	TransportAnnounceKeyUnset   = "cli.transport.announce_key_unset"
	TransportAnnounceKeyInvalid = "cli.transport.announce_key_invalid"
	FlagChannel                 = "cli.flag.channel"
	FlagChannelShort            = "cli.flag.channel_short"
)

func cliKeys() map[Key][]string {
	return map[Key][]string{
		TaskNeedsText:               {ParamVerb},
		ErrorLine:                   {ParamMessage},
		CommandUnknown:              {ParamName},
		WhoamiUnknownArgument:       {ParamArg},
		EnvContinuing:               {ParamErr},
		EnvOpenFailed:               {ParamErr},
		EnvSetFailed:                {ParamKey, ParamErr},
		EnvReadFailed:               {ParamErr},
		TransportAnnounceKeyUnset:   nil,
		TransportAnnounceKeyInvalid: {ParamErr},
		FlagChannel:                 nil,
		FlagChannelShort:            nil,
	}
}
