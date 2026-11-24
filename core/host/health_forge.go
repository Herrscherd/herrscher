package host

import (
	"context"
	"errors"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/forgecreds"
	"github.com/Herrscherd/herrscher/core/forgeport"
	"github.com/Herrscherd/herrscher/core/health"
)

const (
	forgeRateLimitLow = 100
	healthParamRepo   = "repo"
)

func gitHubConnectCommand() string {
	return "herrscher connect github"
}

func forgeCredentials(creds *forgecreds.Credentials) contracts.ForgeCredentials {
	if creds == nil {
		return nil
	}
	return creds
}

// SessionOrigin returns the origin URL of the git checkout at dir, or an empty
// string when dir is not a checkout with an origin.
func SessionOrigin(ctx context.Context, dir string) string {
	origin, err := originOf(ctx, dir)
	if err != nil {
		return ""
	}
	return origin
}

func forgeProbes(inputs healthInputs) []health.Probe {
	if inputs.forge == nil || !forgeBuilt(forgeport.KindGitHub) {
		return nil
	}
	open := func(ctx context.Context, host contracts.ForgeHost) (contracts.Forge, error) {
		return forgeport.Open(ctx, forgeport.KindGitHub, host, inputs.forge)
	}
	return []health.Probe{{
		Check: health.Check{Group: health.GroupForge, Name: forgeport.KindGitHub},
		Watch: githubWatch(open, inputs.repo),
	}}
}

type forgeOpener func(ctx context.Context, host contracts.ForgeHost) (contracts.Forge, error)

func forgeBuilt(kind string) bool {
	forges := contracts.Default.Forges()
	for index := range forges {
		if forges[index].Manifest.Kind == kind && forges[index].Forge != nil {
			return true
		}
	}
	return false
}

func githubWatch(open forgeOpener, origin string) func(context.Context, bool) (contracts.ProbeReport, health.Facts) {
	return func(ctx context.Context, _ bool) (contracts.ProbeReport, health.Facts) {
		host, err := contracts.ParseForgeHost(forgeport.GitHubHost)
		if err != nil {
			return unreachableReport(err), health.Facts{}
		}
		forge, err := open(ctx, host)
		if err != nil {
			return unreachableReport(err), health.Facts{}
		}
		connection, err := forge.Connection(ctx)
		if err != nil {
			return forgeFailure(err), health.Facts{}
		}
		facts := &health.Forge{Source: connection.Source, Remaining: connection.RateLimit.Remaining, Reset: connection.RateLimit.Reset}
		facts.Repo, facts.Permission = sessionRepoAccess(ctx, forge, host, origin)
		return forgeReady(connection), health.Facts{Forge: facts}
	}
}

func sessionRepoAccess(ctx context.Context, forge contracts.Forge, host contracts.ForgeHost, origin string) (string, string) {
	if origin == "" {
		return "", ""
	}
	ref, err := contracts.ParseRepoRef(origin)
	if err != nil || ref.Host() != host {
		return "", ""
	}
	name := ref.Namespace() + "/" + ref.Name()
	permission, err := forge.RepoAccess(ctx, ref)
	if err != nil {
		return name, ""
	}
	return name, permission.String()
}

func forgeFailure(err error) contracts.ProbeReport {
	var limited *contracts.ForgeRateLimitedError
	switch {
	case errors.Is(err, contracts.ErrForgeNotConnected):
		return contracts.ProbeReport{State: contracts.HealthDegraded, Detail: contracts.Notice{Code: contracts.CodeHealthNotLoggedIn}, Remedy: gitHubConnectRemedy()}
	case errors.Is(err, contracts.ErrForgeUnauthorized):
		return contracts.ProbeReport{State: contracts.HealthDown, Detail: contracts.Notice{Code: contracts.CodeHealthCredentialsRefused}, Remedy: gitHubConnectRemedy()}
	case errors.As(err, &limited):
		return contracts.ProbeReport{State: contracts.HealthDown, Detail: contracts.Notice{Code: contracts.CodeHealthRateLimited}}
	}
	return unreachableReport(err)
}

func forgeReady(connection contracts.ForgeConnection) contracts.ProbeReport {
	if connection.RateLimit.Remaining < forgeRateLimitLow {
		return contracts.ProbeReport{State: contracts.HealthDegraded, Detail: contracts.Notice{Code: contracts.CodeHealthRateLimited}}
	}
	if connection.Login == "" {
		return contracts.ProbeReport{State: contracts.HealthOK, Detail: contracts.Notice{Code: contracts.CodeHealthConnected}}
	}
	return contracts.ProbeReport{State: contracts.HealthOK, Detail: contracts.Notice{Code: contracts.CodeHealthSignedInAs, Args: []contracts.Arg{contracts.Text("Name", connection.Login)}}}
}

func gitHubConnectRemedy() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeHealthRemedyRun, Args: []contracts.Arg{contracts.Text("Command", gitHubConnectCommand())}}
}
