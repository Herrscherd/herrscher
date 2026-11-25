package health

import contracts "github.com/Herrscherd/herrscher-contracts"

const forgeAbsoluteQuotaFloor = 100

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
