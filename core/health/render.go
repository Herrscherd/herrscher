package health

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const (
	tabPadding   = 2
	summarySep   = " · "
	daysInterval = 24 * time.Hour
)

// Table writes the banner of the daemon, the checks grouped as the report lists
// them, one line each, and the summary line that counts the checks per state and
// names the worst one when something fails.
func (s Snapshot) Table(writer io.Writer, lang i18n.Language) error {
	text := &renderer{lang: lang}
	text.line(writer, "%s\n", text.key(clitext.HealthBanner, s.bannerParams(text)))
	tabs := tabwriter.NewWriter(writer, 0, 0, tabPadding, ' ', 0)
	text.line(tabs, "%s\n", text.key(clitext.HealthHeader, nil))
	for _, group := range Groups() {
		checks := s.inGroup(group)
		if len(checks) == 0 {
			continue
		}
		text.line(tabs, "%s\n", text.key(groupKey(group), nil))
		for index := range checks {
			report := Settled(checks[index].Report)
			text.line(tabs, "  %s\t%s\t%s\t%s\n", label(checks[index], text), stateText(report.State, text), latency(report), s.facts(checks[index], text, report))
		}
	}
	text.keep(tabs.Flush())
	text.line(writer, "%s\n", s.summary(text))
	return text.err
}

// Details writes the detail view of checks: the state and facts of each one, then
// its cause, and its remedy when there is one.
func (s Snapshot) Details(writer io.Writer, checks []Check, lang i18n.Language) error {
	text := &renderer{lang: lang}
	for index := range checks {
		check := checks[index]
		report := Settled(check.Report)
		text.line(writer, "%s  %s  %s\n", label(check, text), stateText(report.State, text), latency(report))
		if line := s.facts(check, text, report); line != "" {
			text.line(writer, "  %s\n", line)
		}
		if report.Detail.Code != 0 {
			text.line(writer, "  %s\n", text.notice(report.Detail))
		}
		if report.Remedy.Code != 0 {
			text.line(writer, "  -> %s\n", text.notice(report.Remedy))
		}
	}
	return text.err
}

func (s Snapshot) bannerParams(text *renderer) clitext.Params {
	return clitext.Params{
		clitext.ParamVersion: s.Daemon.Version,
		clitext.ParamUptime:  s.uptime(text),
		clitext.ParamPID:     s.Daemon.PID,
		clitext.ParamElapsed: s.Elapsed.Round(time.Millisecond).String(),
	}
}

func (s Snapshot) uptime(text *renderer) string {
	var elapsed time.Duration
	if !s.Daemon.Started.IsZero() && s.ProbedAt.After(s.Daemon.Started) {
		elapsed = s.ProbedAt.Sub(s.Daemon.Started)
	}
	return text.key(clitext.HealthUptime, clitext.Params{
		clitext.ParamDays:    int(elapsed / daysInterval),
		clitext.ParamHours:   int(elapsed % daysInterval / time.Hour),
		clitext.ParamMinutes: int(elapsed % time.Hour / time.Minute),
	})
}

func (s Snapshot) summary(text *renderer) string {
	summary := Summarize(s.Checks)
	var segments []string
	if count := summary.Count(contracts.HealthDown); count > 0 {
		segments = append(segments, text.key(clitext.HealthSummaryDown, clitext.Params{clitext.ParamCount: count}))
	}
	if count := summary.Count(contracts.HealthDegraded); count > 0 {
		segments = append(segments, text.key(clitext.HealthSummaryDegraded, clitext.Params{clitext.ParamCount: count}))
	}
	segments = append(segments, text.key(clitext.HealthSummaryOK, clitext.Params{clitext.ParamCount: summary.Count(contracts.HealthOK)}))
	if worst, found := s.worst(); found {
		segments = append(segments, text.key(clitext.HealthDetailHint, clitext.Params{clitext.ParamName: worst.Service()}))
	}
	return strings.Join(segments, summarySep)
}

func (s Snapshot) worst() (Check, bool) {
	for _, state := range []contracts.HealthState{contracts.HealthDown, contracts.HealthDegraded} {
		for index := range s.Checks {
			if Settled(s.Checks[index].Report).State == state {
				return s.Checks[index], true
			}
		}
	}
	return Check{}, false
}

func (s Snapshot) inGroup(group Group) []Check {
	var checks []Check
	for index := range s.Checks {
		check := s.Checks[index]
		if check.Group == group {
			checks = append(checks, check)
		}
	}
	return checks
}

func (s Snapshot) facts(check Check, text *renderer, report contracts.ProbeReport) string {
	if report.Detail.Code != 0 {
		return text.notice(report.Detail)
	}
	if check.Facts.Pool != nil {
		return poolFacts(check.Facts.Pool, text)
	}
	if check.Group == GroupCore && check.Name == "proxy" {
		return text.key(clitext.HealthProxyFacts, clitext.Params{
			clitext.ParamListen: check.Facts.Listen,
			clitext.ParamCount:  check.Facts.Providers,
		})
	}
	if check.Group == GroupCore && check.Name == "daemon" {
		return text.key(clitext.HealthDaemonFacts, clitext.Params{
			clitext.ParamPID:    s.Daemon.PID,
			clitext.ParamSocket: s.Daemon.Socket,
		})
	}
	return ""
}

func poolFacts(pool *Pool, text *renderer) string {
	segments := []string{text.key(clitext.HealthPoolFacts, clitext.Params{
		clitext.ParamCount: pool.Usable,
		clitext.ParamTotal: pool.Keys,
	})}
	if pool.LastStatus != 0 {
		segments = append(segments, text.key(clitext.HealthPoolLastStatus, clitext.Params{clitext.ParamStatus: pool.LastStatus}))
	}
	if pool.CoolingFor > 0 {
		segments = append(segments, text.key(clitext.HealthPoolCooling, clitext.Params{clitext.ParamDuration: pool.CoolingFor.Round(time.Second).String()}))
	}
	return strings.Join(segments, summarySep)
}

func label(check Check, text *renderer) string {
	if check.Default {
		return check.Service() + " (" + text.key(clitext.HealthDefaultAccount, nil) + ")"
	}
	return check.Service()
}

func latency(report contracts.ProbeReport) string {
	if report.Latency == 0 {
		return "-"
	}
	return report.Latency.Round(time.Millisecond).String()
}

func groupKey(group Group) clitext.Key {
	switch group {
	case GroupCore:
		return clitext.HealthGroupCore
	case GroupGateways:
		return clitext.HealthGroupGateways
	case GroupBackends:
		return clitext.HealthGroupBackends
	case GroupCompaction:
		return clitext.HealthGroupCompaction
	case GroupMemory:
		return clitext.HealthGroupMemory
	case GroupKeys:
		return clitext.HealthGroupKeys
	}
	return ""
}

func stateText(state contracts.HealthState, text *renderer) string {
	return stateGlyph(state) + " " + text.key(stateKey(state), nil)
}

func stateGlyph(state contracts.HealthState) string {
	switch state {
	case contracts.HealthOK:
		return "●"
	case contracts.HealthDegraded:
		return "◐"
	case contracts.HealthDown:
		return "✕"
	case contracts.HealthOff:
		return "○"
	}
	return "○"
}

func stateKey(state contracts.HealthState) clitext.Key {
	switch state {
	case contracts.HealthOK:
		return clitext.HealthStateOK
	case contracts.HealthDegraded:
		return clitext.HealthStateDegraded
	case contracts.HealthDown:
		return clitext.HealthStateDown
	case contracts.HealthOff:
		return clitext.HealthStateOff
	}
	return clitext.HealthStateOff
}

type renderer struct {
	lang i18n.Language
	err  error
}

func (r *renderer) key(key clitext.Key, params clitext.Params) string {
	value, err := clitext.Text(r.lang, key, params)
	r.keep(err)
	return value
}

func (r *renderer) notice(notice contracts.Notice) string {
	value, err := contracts.NewRenderer().Render(r.lang, notice)
	r.keep(err)
	return value
}

func (r *renderer) line(w io.Writer, format string, args ...any) {
	_, err := fmt.Fprintf(w, format, args...)
	r.keep(err)
}

func (r *renderer) keep(err error) {
	if r.err == nil {
		r.err = err
	}
}
