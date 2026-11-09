// Package health is the model of `herrscher health`: the checks the daemon runs,
// their aggregate and the exit code a client derives from it.
package health

import (
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	// ServiceDaemon names the check of the daemon itself, in [GroupCore].
	ServiceDaemon = "daemon"
	// ServiceProxy names the check of the key proxy, in [GroupCore].
	ServiceProxy = "proxy"
)

// Check is one service checked, for one account when the service has accounts.
type Check struct {
	Group   Group
	Name    string
	Account contracts.ProfileName
	Default bool
	Report  contracts.ProbeReport
	Facts   Facts
}

// Facts are what the host knows about one of its own services, rendered by the
// client next to the report when the report carries no detail.
type Facts struct {
	Socket            string
	Listen            string
	Providers         int
	Pool              *Pool
	TimedOut          time.Duration
	Compaction        *Compaction
	CompactionPartial bool
	CompactionUnknown bool
}

// Compaction is the last run of a compaction stage the host recorded.
type Compaction struct {
	At      time.Time
	Outcome contracts.CompactOutcome
}

// Pool is the state of one provider key pool of the key proxy.
type Pool struct {
	Keys       int
	Usable     int
	LastStatus int
	CoolingFor time.Duration
}

// Cooling returns how many keys of the pool are cooling down.
func (p Pool) Cooling() int {
	return p.Keys - p.Usable
}

// Daemon describes the daemon that ran the checks.
type Daemon struct {
	PID     int
	Started time.Time
	Socket  string
	Version string
}

// Snapshot is one complete health run, as the daemon answers it.
type Snapshot struct {
	Daemon       Daemon
	ProbedAt     time.Time
	Elapsed      time.Duration
	Checks       []Check
	CompactStack []string
}

// Service returns the name a client selects the check by: the plugin name, and
// the account after a colon when the check is for a named account.
func (c Check) Service() string {
	if c.Account == "" {
		return c.Name
	}
	return c.Name + ":" + string(c.Account)
}

// Matches reports whether service names this check: its [Check.Service], or its
// plain name, compared without case.
func (c Check) Matches(service string) bool {
	return strings.EqualFold(c.Service(), service) || strings.EqualFold(c.Name, service)
}

// Find returns the checks service names, in report order.
func (s Snapshot) Find(service string) []Check {
	var found []Check
	for index := range s.Checks {
		if s.Checks[index].Matches(service) {
			found = append(found, s.Checks[index])
		}
	}
	return found
}

// Summary is the aggregate of a set of checks.
type Summary struct {
	Counts map[contracts.HealthState]int
	Worst  contracts.HealthState
}

// Summarize counts the checks per state and keeps the worst one.
func Summarize(checks []Check) Summary {
	summary := Summary{Counts: make(map[contracts.HealthState]int, len(contracts.HealthStates())), Worst: contracts.HealthOff}
	states := make([]contracts.HealthState, 0, len(checks))
	for index := range checks {
		state := Settled(checks[index].Report).State
		summary.Counts[state]++
		states = append(states, state)
	}
	summary.Worst = contracts.WorstHealth(states...)
	return summary
}

// Count returns how many checks ended in state.
func (s Summary) Count(state contracts.HealthState) int {
	return s.Counts[state]
}

// Watched returns how many checks can fail, every check but the ones that are
// off.
func (s Summary) Watched() int {
	total := 0
	for state, count := range s.Counts {
		if state != contracts.HealthOff {
			total += count
		}
	}
	return total
}

// ExitCode returns the exit code the summary maps to.
func (s Summary) ExitCode() ExitCode {
	switch s.Worst {
	case contracts.HealthDown:
		return ExitDown
	case contracts.HealthDegraded:
		return ExitDegraded
	case contracts.HealthOK, contracts.HealthOff:
		return ExitHealthy
	}
	return ExitHealthy
}

// Settled returns report with a state: a report a probe left without one is
// down, since it is not a report at all.
func Settled(report contracts.ProbeReport) contracts.ProbeReport {
	if _, err := report.State.MarshalText(); err != nil {
		report.State = contracts.HealthDown
	}
	return report
}

// ExitCode is the exit status of `herrscher health`.
type ExitCode int

const (
	// ExitHealthy is nothing degraded and nothing down.
	ExitHealthy ExitCode = 0
	// ExitDown is something down, or a daemon that cannot be reached.
	ExitDown ExitCode = 1
	// ExitDegraded is a worst state of degraded.
	ExitDegraded ExitCode = 2
)
