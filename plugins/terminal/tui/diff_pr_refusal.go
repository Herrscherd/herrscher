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

func forgeRefusal(check *health.Check, trans translator) error {
	if check.Report.State == contracts.HealthOK {
		return nil
	}
	code := check.Report.Detail.Code
	if code == contracts.CodeHealthRateLimited {
		return rateLimitRefusal(check.Facts.Forge, trans)
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

func rateLimitRefusal(forge *health.Forge, trans translator) error {
	if forge == nil || forge.Quota == nil || forge.Quota.Reset.IsZero() {
		return errForgeRateLimited
	}
	return keyedError{cause: errForgeRateLimited, key: "tui.diffgit.forge_rate_limited_reset", params: map[string]any{"Reset": forge.Quota.Reset.UTC().Format(time.RFC3339)}, catalog: trans.catalog}
}

func missingForgeRefusal(trans translator) error {
	return keyedError{cause: forgeport.ErrNoPlugin, key: "tui.diffgit.forge_missing", catalog: trans.catalog}
}
