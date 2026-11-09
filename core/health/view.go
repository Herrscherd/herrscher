package health

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const (
	factsSep        = " · "
	daySpan         = 24 * time.Hour
	secondDecimals  = 1
	tenthsPerSecond = 10
	stackSep        = " → "
	homeMark        = "~"
)

// Row is one check as a report shows it, every text already rendered in the
// language of the report.
type Row struct {
	Check     Check
	State     contracts.HealthState
	Glyph     string
	StateText string
	Name      string
	Account   string
	Default   bool
	Latency   string
	Cause     string
	Facts     string
	Remedy    string
}

// Detail returns the one line a table shows for the row: its cause, else what the
// host knows of the service, else the remedy.
func (r Row) Detail() string {
	for _, text := range []string{r.Cause, r.Facts} {
		if text != "" {
			return text
		}
	}
	return r.Remedy
}

// Section is one group of a report and the rows of its checks.
type Section struct {
	Group Group
	Title string
	Rows  []Row
}

// Count is the number of checks in one state, as the summary line words it.
type Count struct {
	State contracts.HealthState
	Text  string
}

// View is a snapshot rendered in one language: the pieces a client lays out and
// paints as it likes.
type View struct {
	Title    string
	Banner   string
	Header   []string
	Sections []Section
	Counts   []Count
	Hint     string
	Worst    string
}

// View renders the snapshot in lang: the banner, the checks grouped in report
// order, and the summary counts with the service the hint names when something
// fails.
func (s Snapshot) View(lang i18n.Language) (View, error) {
	text := &renderer{lang: lang}
	view := View{
		Title:  text.key(clitext.HealthBannerTitle, clitext.Params{clitext.ParamVersion: s.Daemon.Version}),
		Banner: text.key(clitext.HealthBanner, s.bannerParams(text)),
		Header: strings.Split(text.key(clitext.HealthHeader, nil), "\t"),
		Counts: s.counts(text),
		Hint:   text.key(clitext.HealthDetailHint, nil),
	}
	for _, group := range Groups() {
		checks := s.inGroup(group)
		if len(checks) == 0 {
			continue
		}
		view.Sections = append(view.Sections, Section{Group: group, Title: s.groupTitle(group, text), Rows: s.rows(checks, text)})
	}
	if worst, found := s.worst(); found {
		view.Worst = worst.Service()
	}
	return view, text.err
}

// Rows renders checks in lang, one row each, in the order given.
func (s Snapshot) Rows(checks []Check, lang i18n.Language) ([]Row, error) {
	text := &renderer{lang: lang}
	return s.rows(checks, text), text.err
}

// Latency renders a measured duration the way a report shows it: milliseconds
// under a second, seconds with one decimal under a minute, a compact span beyond.
func Latency(lang i18n.Language, elapsed time.Duration) (string, error) {
	text := &renderer{lang: lang}
	return text.latency(elapsed), text.err
}

func (s Snapshot) rows(checks []Check, text *renderer) []Row {
	rows := make([]Row, 0, len(checks))
	for index := range checks {
		rows = append(rows, s.row(checks[index], text))
	}
	return rows
}

func (s Snapshot) row(check Check, text *renderer) Row {
	report := Settled(check.Report)
	row := Row{
		Check:     check,
		State:     report.State,
		Glyph:     stateGlyph(report.State),
		StateText: text.key(stateKey(report.State), nil),
		Name:      checkName(check, text),
		Account:   accountName(check, text),
		Default:   check.Default,
		Facts:     s.facts(check, text),
	}
	if report.Latency > 0 {
		row.Latency = text.latency(report.Latency)
	}
	if report.Detail.Code != 0 {
		row.Cause = text.notice(report.Detail)
	}
	if report.Remedy.Code != 0 {
		row.Remedy = text.notice(report.Remedy)
	}
	return row
}

func (s Snapshot) groupTitle(group Group, text *renderer) string {
	title := text.key(groupKey(group), nil)
	if group != GroupCompaction || len(s.CompactStack) == 0 {
		return title
	}
	return title + "  " + text.key(clitext.HealthCompactStack, clitext.Params{clitext.ParamStack: strings.Join(s.CompactStack, stackSep)})
}

func (s Snapshot) bannerParams(text *renderer) clitext.Params {
	var uptime time.Duration
	if !s.Daemon.Started.IsZero() && s.ProbedAt.After(s.Daemon.Started) {
		uptime = s.ProbedAt.Sub(s.Daemon.Started)
	}
	return clitext.Params{
		clitext.ParamUptime:  text.span(uptime),
		clitext.ParamPID:     s.Daemon.PID,
		clitext.ParamElapsed: text.latency(s.Elapsed),
	}
}

func (s Snapshot) counts(text *renderer) []Count {
	summary := Summarize(s.Checks)
	var counts []Count
	for _, state := range []contracts.HealthState{contracts.HealthDown, contracts.HealthDegraded} {
		if count := summary.Count(state); count > 0 {
			counts = append(counts, Count{State: state, Text: text.key(summaryKey(state), clitext.Params{clitext.ParamCount: count})})
		}
	}
	ok := summary.Count(contracts.HealthOK)
	return append(counts, Count{State: contracts.HealthOK, Text: text.key(clitext.HealthSummaryOK, clitext.Params{clitext.ParamCount: ok})})
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
		if s.Checks[index].Group == group {
			checks = append(checks, s.Checks[index])
		}
	}
	return checks
}

func (s Snapshot) facts(check Check, text *renderer) string {
	switch {
	case check.Facts.Pool != nil:
		return poolFacts(check.Facts.Pool, text)
	case check.Group == GroupCore && check.Name == ServiceProxy:
		return text.key(clitext.HealthProxyFacts, clitext.Params{
			clitext.ParamListen: check.Facts.Listen,
			clitext.ParamCount:  check.Facts.Providers,
		})
	case check.Group == GroupCompaction:
		return compactionFacts(check.Facts, text)
	case check.Group == GroupCore && check.Name == ServiceDaemon:
		if check.Facts.Socket != "" {
			return homeRelative(check.Facts.Socket)
		}
		return homeRelative(s.Daemon.Socket)
	}
	return ""
}

func compactionFacts(facts Facts, text *renderer) string {
	fact := text.key(clitext.HealthCompactNever, nil)
	if last := facts.Compaction; last != nil {
		fact = text.key(clitext.HealthCompactLast, clitext.Params{
			clitext.ParamOutcome: text.outcome(last.Outcome),
			clitext.ParamDate:    last.At.Local().Format(time.DateTime),
		})
	}
	if facts.CompactionPartial {
		return fact + factsSep + text.key(clitext.HealthCompactPartial, nil)
	}
	return fact
}

func (r *renderer) outcome(outcome contracts.CompactOutcome) string {
	key, known := clitext.CompactOutcomeKey(outcome)
	if !known {
		return outcome.String()
	}
	return r.key(key, nil)
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
		segments = append(segments, text.key(clitext.HealthPoolCooling, clitext.Params{clitext.ParamDuration: text.span(pool.CoolingFor)}))
	}
	return strings.Join(segments, factsSep)
}

func checkName(check Check, text *renderer) string {
	if check.Group != GroupCore {
		return check.Name
	}
	switch check.Name {
	case ServiceDaemon:
		return text.key(clitext.HealthNameDaemon, nil)
	case ServiceProxy:
		return text.key(clitext.HealthNameProxy, nil)
	}
	return check.Name
}

func accountName(check Check, text *renderer) string {
	if check.Account != "" {
		return string(check.Account)
	}
	if check.Group == GroupBackends {
		return text.key(clitext.QuotaAccountNative, nil)
	}
	return ""
}

func (r *renderer) latency(elapsed time.Duration) string {
	millis := max(elapsed.Round(time.Millisecond), time.Millisecond)
	if millis < time.Second {
		return r.key(clitext.HealthSpanMillis, clitext.Params{clitext.ParamDuration: int(millis / time.Millisecond)})
	}
	tenths := elapsed.Round(time.Second / tenthsPerSecond)
	if tenths < time.Minute {
		seconds := clitext.FormatNumber(r.lang, tenths.Seconds(), secondDecimals)
		return r.key(clitext.HealthSpanSeconds, clitext.Params{clitext.ParamSeconds: seconds})
	}
	return r.span(elapsed)
}

func (r *renderer) span(elapsed time.Duration) string {
	switch {
	case elapsed >= daySpan:
		return r.key(clitext.HealthSpanDays, clitext.Params{
			clitext.ParamDays:  int(elapsed / daySpan),
			clitext.ParamHours: int(elapsed % daySpan / time.Hour),
		})
	case elapsed >= time.Hour:
		return r.key(clitext.HealthSpanHours, clitext.Params{
			clitext.ParamHours:   int(elapsed / time.Hour),
			clitext.ParamMinutes: int(elapsed % time.Hour / time.Minute),
		})
	case elapsed >= time.Minute:
		return r.key(clitext.HealthSpanMinutes, clitext.Params{clitext.ParamMinutes: int(elapsed / time.Minute)})
	}
	return r.key(clitext.HealthSpanSeconds, clitext.Params{clitext.ParamSeconds: int(elapsed / time.Second)})
}

func summaryKey(state contracts.HealthState) clitext.Key {
	if state == contracts.HealthDown {
		return clitext.HealthSummaryDown
	}
	return clitext.HealthSummaryDegraded
}

func homeRelative(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return homeMark + string(filepath.Separator) + rest
	}
	return path
}
