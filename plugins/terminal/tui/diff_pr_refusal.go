package tui

import (
	"errors"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/forgeport"
	"github.com/Herrscherd/herrscher/core/health"
)

var (
	errForgeRateLimited    = errors.New("GitHub rate limit reached")
	errForgeUnreachable    = errors.New("GitHub is unreachable from this host")
	errForgeRepoUnreadable = errors.New("GitHub cannot read this repository with this connection")
)

func forgeRefusal(check *health.Check, trans translator, now time.Time) error {
	if check.Report.State == contracts.HealthOK {
		return nil
	}
	code := check.Report.Detail.Code
	if code == contracts.CodeHealthRateLimited {
		return rateLimitRefusal(refusalReset(check.Facts), trans, now)
	}
	if code == contracts.CodeHealthNotLoggedIn {
		return contracts.ErrForgeNotConnected
	}
	if code == contracts.CodeHealthCredentialsRefused {
		return contracts.ErrForgeUnauthorized
	}
	if code == contracts.CodeHealthRepoAccessUnreadable {
		return errForgeRepoUnreadable
	}
	return errForgeUnreachable
}

func refusalReset(facts health.Facts) time.Time {
	if forge := facts.Forge; forge != nil && forge.Quota != nil && !forge.Quota.Reset.IsZero() {
		return forge.Quota.Reset
	}
	return facts.ForgeReset
}

func rateLimitRefusal(reset time.Time, trans translator, now time.Time) error {
	if reset.IsZero() {
		return errForgeRateLimited
	}
	return keyedError{cause: errForgeRateLimited, key: "tui.diffgit.forge_rate_limited_reset", params: map[string]any{"Reset": resetLabel(reset, now)}, catalog: trans.catalog}
}

func resetLabel(reset, now time.Time) string {
	reset = reset.UTC()
	if reset.Format(time.DateOnly) == now.UTC().Format(time.DateOnly) {
		return reset.Format("15:04 MST")
	}
	return reset.Format("2006-01-02 15:04 MST")
}

func missingForgeRefusal(trans translator) error {
	return keyedError{cause: forgeport.ErrNoPlugin, key: "tui.diffgit.forge_missing", catalog: trans.catalog}
}
