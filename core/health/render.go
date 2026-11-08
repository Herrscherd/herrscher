package health

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const tabPadding = 2

// Table writes the checks grouped as the report lists them, one line each, and
// the summary line that counts the watched checks that are ok.
func (s Snapshot) Table(writer io.Writer, lang i18n.Language) error {
	text := &renderer{lang: lang}
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
			text.line(tabs, "  %s\t%s\t%s\t%s\n", label(checks[index], text), text.key(stateKey(report.State), nil), latency(report), s.facts(checks[index], text, report))
		}
	}
	text.keep(tabs.Flush())
	return text.write(writer, clitext.HealthSummary, s.summaryParams())
}

// Details writes the detail view of checks: the cause of each one, then the
// remedy when there is one.
func (s Snapshot) Details(writer io.Writer, checks []Check, lang i18n.Language) error {
	text := &renderer{lang: lang}
	for index := range checks {
		check := checks[index]
		report := Settled(check.Report)
		text.line(writer, "%s  %s  %s\n", label(check, text), text.key(stateKey(report.State), nil), latency(report))
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

func (s Snapshot) summaryParams() clitext.Params {
	summary := Summarize(s.Checks)
	return clitext.Params{
		clitext.ParamCount: summary.Count(contracts.HealthOK),
		clitext.ParamTotal: summary.Watched(),
	}
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
		return text.key(clitext.HealthPoolFacts, clitext.Params{
			clitext.ParamCount: check.Facts.Pool.Usable,
			clitext.ParamTotal: check.Facts.Pool.Keys,
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

func (r *renderer) write(writer io.Writer, key clitext.Key, params clitext.Params) error {
	value := r.key(key, params)
	if _, err := fmt.Fprintln(writer, value); err != nil {
		return err
	}
	return r.err
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
