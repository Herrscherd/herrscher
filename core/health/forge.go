package health

import (
	"errors"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const forgeAbsoluteQuotaFloor = 100

const gitHubConnectCommand = "herrscher connect github"

// ForgeReport is the report of a connected forge: healthy when the connection
// answers, whether or not it names an account, and degraded when the rate limit
// it reports is nearly spent.
func ForgeReport(connection contracts.ForgeConnection) contracts.ProbeReport {
	if knownQuota(connection.RateLimit) && connection.RateLimit.Remaining < forgeAbsoluteQuotaFloor {
		return contracts.ProbeReport{State: contracts.HealthDegraded, Detail: contracts.Notice{Code: contracts.CodeHealthRateLimited}}
	}
	if connection.Login == "" {
		return contracts.ProbeReport{State: contracts.HealthOK, Detail: contracts.Notice{Code: contracts.CodeHealthConnected}}
	}
	return contracts.ProbeReport{State: contracts.HealthOK, Detail: contracts.Notice{Code: contracts.CodeHealthSignedInAs, Args: []contracts.Arg{contracts.Text("Name", connection.Login)}}}
}

// ForgeFailure is the report of a forge that gave no connection, from the error
// it returned: a missing credential is degraded, a refused credential and a rate
// limit are down, and any other failure is unreachable.
func ForgeFailure(err error) contracts.ProbeReport {
	var limited *contracts.ForgeRateLimitedError
	switch {
	case errors.Is(err, contracts.ErrForgeNotConnected):
		return contracts.ProbeReport{State: contracts.HealthDegraded, Detail: contracts.Notice{Code: contracts.CodeHealthNotLoggedIn}, Remedy: connectRemedy()}
	case errors.Is(err, contracts.ErrForgeUnauthorized):
		return contracts.ProbeReport{State: contracts.HealthDown, Detail: contracts.Notice{Code: contracts.CodeHealthCredentialsRefused}, Remedy: connectRemedy()}
	case errors.As(err, &limited):
		return contracts.ProbeReport{State: contracts.HealthDown, Detail: contracts.Notice{Code: contracts.CodeHealthRateLimited}}
	}
	return Unreachable(err)
}

// ForgeFailureFacts are the facts of a forge that gave no connection: the time the
// rate limit lifts when the forge refused the call for its rate limit.
func ForgeFailureFacts(err error) Facts {
	var limited *contracts.ForgeRateLimitedError
	if errors.As(err, &limited) {
		return Facts{ForgeReset: limited.Reset}
	}
	return Facts{}
}

// Unreachable is the report of a service that could not be asked, with the
// technical reason.
func Unreachable(err error) contracts.ProbeReport {
	return contracts.ProbeReport{State: contracts.HealthDown, Detail: contracts.Notice{Code: contracts.CodeHealthUnreachable, Args: []contracts.Arg{contracts.Text("Reason", err.Error())}}}
}

// WithRepoAccess degrades a healthy forge report when the repository asked about
// cannot be read with the connection; any other report is returned unchanged.
func WithRepoAccess(report contracts.ProbeReport, repo string, err error) contracts.ProbeReport {
	if report.State != contracts.HealthOK {
		return report
	}
	return contracts.ProbeReport{State: contracts.HealthDegraded, Detail: repoAccessNotice(repo, err)}
}

// QuotaOf is the quota a forge reported, or nil when it reported none.
func QuotaOf(limit contracts.ForgeRateLimit) *Quota {
	if !knownQuota(limit) {
		return nil
	}
	return &Quota{Remaining: limit.Remaining, Reset: limit.Reset}
}

func knownQuota(limit contracts.ForgeRateLimit) bool {
	return limit.Remaining != 0 || !limit.Reset.IsZero()
}

func repoAccessNotice(repo string, err error) contracts.Notice {
	return contracts.Notice{Code: contracts.CodeHealthRepoAccessUnreadable, Args: []contracts.Arg{contracts.Text("Repo", repo), contracts.Text("Reason", err.Error())}}
}

func connectRemedy() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeHealthRemedyRun, Args: []contracts.Arg{contracts.Text("Command", gitHubConnectCommand)}}
}
