package clitext

// Message keys of the CI loop nudges and notices sent to a session.
const (
	SessionCINudge        = "cli.session.ci.nudge"
	SessionCIStop         = "cli.session.ci.stop"
	SessionCIUnlocated    = "cli.session.ci.unlocated"
	SessionCINoLog        = "cli.session.ci.no_log"
	SessionCIRefundFailed = "cli.session.ci.refund_failed"
	ParamAttempt          = "Attempt"
	ParamMax              = "Max"
	ParamLog              = "Log"
	ParamTag              = "Tag"
	ParamCause            = "Cause"
)

func sessionCIKeys() map[Key][]string {
	return map[Key][]string{
		SessionCINudge:        {ParamNumber, ParamAttempt, ParamMax, ParamTag, ParamLog},
		SessionCIStop:         {ParamNumber, ParamMax},
		SessionCIUnlocated:    {ParamNumber},
		SessionCINoLog:        nil,
		SessionCIRefundFailed: {ParamNumber, ParamAttempt, ParamCause},
	}
}
