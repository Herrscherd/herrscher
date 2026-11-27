package clitext

// Message keys of the CI loop nudges and notices sent to a session.
const (
	SessionCINudge     = "cli.session.ci.nudge"
	SessionCIStop      = "cli.session.ci.stop"
	SessionCIUnlocated = "cli.session.ci.unlocated"
	SessionCINoLog     = "cli.session.ci.no_log"
	ParamAttempt       = "Attempt"
	ParamMax           = "Max"
	ParamLog           = "Log"
)

func sessionCIKeys() map[Key][]string {
	return map[Key][]string{
		SessionCINudge:     {ParamNumber, ParamAttempt, ParamMax, ParamLog},
		SessionCIStop:      {ParamNumber, ParamMax},
		SessionCIUnlocated: {ParamNumber},
		SessionCINoLog:     nil,
	}
}
