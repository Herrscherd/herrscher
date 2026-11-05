package clitext

// Keys of the messages of attached sessions and of the session verbs.
const (
	AttachNotAvailable         = "cli.attach.not_available"
	AttachNoSession            = "cli.attach.no_session"
	SessionListEmpty           = "cli.session.list.empty"
	SessionListHeader          = "cli.session.list.header"
	SessionWhoEmpty            = "cli.session.who.empty"
	SessionWhoHeader           = "cli.session.who.header"
	SessionLogEmpty            = "cli.session.log.empty"
	SessionClosed              = "cli.session.closed"
	SessionArchived            = "cli.session.archived"
	SessionRenamed             = "cli.session.renamed"
	SessionRenameSameName      = "cli.session.rename.same_name"
	SessionChannelLeftOpen     = "cli.session.channel.left_open"
	SessionTranscriptLeft      = "cli.session.rename.transcript_left"
	SessionParticipantsLeft    = "cli.session.rename.participants_left"
	SessionScheduleOwnsSession = "cli.session.rename.schedule_owns"
)

func sessionKeys() map[Key][]string {
	return map[Key][]string{
		AttachNotAvailable:         {ParamVerb},
		AttachNoSession:            nil,
		SessionListEmpty:           nil,
		SessionListHeader:          nil,
		SessionWhoEmpty:            nil,
		SessionWhoHeader:           {ParamName},
		SessionLogEmpty:            nil,
		SessionClosed:              {ParamName},
		SessionArchived:            {ParamName},
		SessionRenamed:             {ParamName, ParamTo},
		SessionRenameSameName:      {ParamName},
		SessionChannelLeftOpen:     {ParamErr},
		SessionTranscriptLeft:      {ParamName, ParamErr},
		SessionParticipantsLeft:    {ParamName, ParamErr},
		SessionScheduleOwnsSession: {ParamSchedule, ParamName},
	}
}
