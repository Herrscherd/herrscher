package clitext

// Message keys of the CI loop nudges and stop notices sent to a session.
const (
	SessionCINudge = "cli.session.ci.nudge"
	SessionCIStop  = "cli.session.ci.stop"
	SessionCINoLog = "cli.session.ci.no_log"
	ParamAttempt   = "Attempt"
	ParamMax       = "Max"
	ParamCheck     = "Check"
	ParamLog       = "Log"
)

func sessionCIKeys() map[Key][]string {
	return map[Key][]string{
		SessionCINudge: {ParamNumber, ParamAttempt, ParamMax, ParamCheck, ParamURL, ParamLog},
		SessionCIStop:  {ParamNumber, ParamMax},
		SessionCINoLog: nil,
	}
}
