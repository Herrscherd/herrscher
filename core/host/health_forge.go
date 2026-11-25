package host

import (
	"context"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/forgecreds"
	"github.com/Herrscherd/herrscher/core/forgeport"
	"github.com/Herrscherd/herrscher/core/health"
)

const (
	forgeRefreshEvery = 5 * time.Minute
	repoRetryEvery    = time.Minute
	healthParamOrigin = "repo"
)

type repoProbe func(ctx context.Context, forge contracts.Forge, host contracts.ForgeHost, origin string) (string, *contracts.RepoPermission, error)

func forgeCredentials(creds *forgecreds.Credentials) contracts.ForgeCredentials {
	if creds == nil {
		return nil
	}
	return creds
}

// CheckoutOrigin returns the origin URL of the git checkout at dir, or an empty
// string when dir is not a checkout with an origin.
func CheckoutOrigin(ctx context.Context, dir string) string {
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
	probeRepo := sessionRepoAccess
	if inputs.watching && inputs.forgeCache != nil {
		probeRepo = inputs.forgeCache.repoAccess(probeRepo)
	}
	run := githubRun(open, inputs.repo, probeRepo)
	if inputs.watching && inputs.forgeCache != nil {
		run = inputs.forgeCache.memo(run, inputs.repo)
	}
	return []health.Probe{{
		Check: health.Check{Group: health.GroupForge, Name: forgeport.KindGitHub},
		Run:   run,
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

func githubRun(open forgeOpener, origin string, probeRepo repoProbe) runFunc {
	return func(ctx context.Context, _ bool) (contracts.ProbeReport, health.Facts) {
		host, err := contracts.ParseForgeHost(forgeport.GitHubHost)
		if err != nil {
			return health.Unreachable(err), health.Facts{}
		}
		forge, err := open(ctx, host)
		if err != nil {
			return health.Unreachable(err), health.Facts{}
		}
		connection, err := forge.Connection(ctx)
		if err != nil {
			return health.ForgeFailure(err), health.Facts{}
		}
		facts := &health.Forge{Source: connection.Source, Login: connection.Login, Quota: health.QuotaOf(connection.RateLimit)}
		repo, permission, repoErr := probeRepo(ctx, forge, host, origin)
		facts.Repo, facts.Permission = repo, permission
		report := health.ForgeReport(connection)
		if repoErr != nil {
			facts.RepoErr = repoErr.Error()
			report = health.WithRepoAccess(report, repo, repoErr)
		}
		return report, health.Facts{Forge: facts}
	}
}

func sessionRepoAccess(ctx context.Context, forge contracts.Forge, host contracts.ForgeHost, origin string) (string, *contracts.RepoPermission, error) {
	ref, ok := originRepo(origin, host)
	if !ok {
		return "", nil, nil
	}
	name := ref.Namespace() + "/" + ref.Name()
	permission, err := forge.RepoAccess(ctx, ref)
	if err != nil {
		return name, nil, err
	}
	return name, &permission, nil
}

func originRepo(origin string, host contracts.ForgeHost) (contracts.ForgeRepoRef, bool) {
	ref, err := contracts.ParseRepoRef(origin)
	return ref, err == nil && ref.Host() == host
}

type forgeCache struct {
	mutex      sync.Mutex
	now        func() time.Time
	origin     string
	fetched    time.Time
	report     contracts.ProbeReport
	facts      health.Facts
	filled     bool
	failedRepo *repoFailure
}

type repoFailure struct {
	origin string
	at     time.Time
	name   string
	err    error
}

func newForgeCache(now func() time.Time) *forgeCache {
	return &forgeCache{now: now}
}

func (c *forgeCache) repoAccess(probe repoProbe) repoProbe {
	return func(ctx context.Context, forge contracts.Forge, host contracts.ForgeHost, origin string) (string, *contracts.RepoPermission, error) {
		if c.repoRetryHeld(origin) {
			return c.failedRepo.name, nil, c.failedRepo.err
		}
		name, permission, err := probe(ctx, forge, host, origin)
		c.failedRepo = nil
		if err != nil && ctx.Err() == nil {
			c.failedRepo = &repoFailure{origin: origin, at: c.now(), name: name, err: err}
		}
		return name, permission, err
	}
}

func (c *forgeCache) repoRetryHeld(origin string) bool {
	return c.failedRepo != nil && c.failedRepo.origin == origin && c.now().Sub(c.failedRepo.at) < repoRetryEvery
}

func (c *forgeCache) memo(run runFunc, origin string) runFunc {
	return func(ctx context.Context, deep bool) (contracts.ProbeReport, health.Facts) {
		c.mutex.Lock()
		defer c.mutex.Unlock()
		if c.fresh(origin) {
			return c.report, c.facts
		}
		report, facts := run(ctx, deep)
		if ctx.Err() == nil && cacheable(facts) {
			c.store(origin, report, facts)
		}
		return report, facts
	}
}

func cacheable(facts health.Facts) bool {
	return facts.Forge != nil && facts.Forge.RepoErr == ""
}

func (c *forgeCache) fresh(origin string) bool {
	return c.filled && c.origin == origin && c.now().Sub(c.fetched) < forgeRefreshEvery
}

func (c *forgeCache) store(origin string, report contracts.ProbeReport, facts health.Facts) {
	c.filled, c.origin, c.fetched, c.report, c.facts = true, origin, c.now(), report, facts
}
