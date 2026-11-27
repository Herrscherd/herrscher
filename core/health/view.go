package health

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/termpaint"
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
	State     contracts.HealthState
	Glyph     termpaint.Printed
	StateText termpaint.Printed
	Name      termpaint.Printed
	Account   termpaint.Printed
	Default   bool
	Latency   termpaint.Printed
	Cause     termpaint.Printed
	Facts     termpaint.Printed
	Remedy    termpaint.Printed
}

// Detail returns the one line a table shows for the row: its cause, else what the
// host knows of the service, else the remedy.
func (r Row) Detail() termpaint.Printed {
	for _, text := range []termpaint.Printed{r.Cause, r.Facts} {
		if text.String() != "" {
			return text
		}
	}
	return r.Remedy
}

// Section is one group of a report and the rows of its checks.
type Section struct {
	Group   Group
	Title   termpaint.Printed
	Heading termpaint.Printed
	Rows    []Row
}

// Count is the number of checks in one state, as the summary line words it.
type Count struct {
	State contracts.HealthState
	Text  termpaint.Printed
}

// View is a snapshot rendered in one language: the pieces a client lays out and
// paints as it likes.
type View struct {
	Title    termpaint.Printed
	Banner   termpaint.Printed
	Header   []termpaint.Printed
	Sections []Section
	Counts   []Count
	Hint     termpaint.Printed
	Worst    termpaint.Printed
}

// View renders the snapshot in lang: the banner, the checks grouped in report
// order, and the summary counts with the service the hint names when something
// fails.
func (s Snapshot) View(messages *clitext.Messages, lang i18n.Language) (View, error) {
	text := newRenderer(messages, lang)
	view := s.buildView(text)
	return view, text.err
}

func (s Snapshot) buildView(text *renderer) View {
	view := View{
		Title:  termpaint.Escape(text.key(clitext.HealthBannerTitle, clitext.Params{clitext.ParamVersion: s.Daemon.Version})),
		Banner: termpaint.Escape(text.key(clitext.HealthBanner, s.bannerParams(text))),
		Header: escapeAll(strings.Split(text.key(clitext.HealthHeader, nil), "\t")),
		Counts: s.counts(text),
		Hint:   termpaint.Escape(text.key(clitext.HealthDetailHint, nil)),
	}
	for _, group := range Groups() {
		checks := s.inGroup(group)
		if len(checks) == 0 {
			continue
		}
		title := s.groupTitle(group, text)
		view.Sections = append(view.Sections, Section{Group: group, Title: termpaint.Escape(title), Heading: termpaint.Escape(strings.ToUpper(title)), Rows: s.buildRows(checks, text)})
	}
	if worst, found := s.worst(); found {
		view.Worst = termpaint.Escape(worst.Service())
	}
	return view
}

func escapeAll(lines []string) []termpaint.Printed {
	escaped := make([]termpaint.Printed, 0, len(lines))
	for _, line := range lines {
		escaped = append(escaped, termpaint.Escape(line))
	}
	return escaped
}

// Rows renders checks in lang, one row each, in the order given.
func (s Snapshot) Rows(messages *clitext.Messages, checks []Check, lang i18n.Language) ([]Row, error) {
	text := newRenderer(messages, lang)
	return s.buildRows(checks, text), text.err
}

// Latency renders a measured duration the way a report shows it: milliseconds
// under a second, seconds with one decimal under a minute, a compact span beyond.
func Latency(messages *clitext.Messages, lang i18n.Language, elapsed time.Duration) (string, error) {
	text := newRenderer(messages, lang)
	return text.latency(elapsed), text.err
}

func (s Snapshot) buildRows(checks []Check, text *renderer) []Row {
	rows := make([]Row, 0, len(checks))
	for index := range checks {
		rows = append(rows, s.row(checks[index], text))
	}
	return rows
}

func (s Snapshot) row(check Check, text *renderer) Row {
	report := Settled(check.Report)
	row := Row{
		State:     report.State,
		Glyph:     termpaint.Escape(stateGlyph(report.State)),
		StateText: termpaint.Escape(text.key(stateKey(report.State), nil)),
		Name:      termpaint.Escape(checkName(check, text)),
		Account:   termpaint.Escape(accountName(check, text)),
		Default:   check.Default,
		Facts:     termpaint.Escape(s.facts(check, text)),
	}
	if report.Latency > 0 {
		row.Latency = termpaint.Escape(text.latency(report.Latency))
	}
	if report.Detail.Code != 0 {
		row.Cause = termpaint.Escape(causeText(check, report, text))
	}
	if report.Remedy.Code != 0 {
		row.Remedy = termpaint.Escape(text.notice(report.Remedy))
	}
	return row
}

func (s Snapshot) groupTitle(group Group, text *renderer) string {
	title := text.key(groupKey(group), nil)
	if group != GroupCompaction || len(s.CompactStack) == 0 {
		return title
	}
	stack := strings.Join(s.CompactStack, stackSep)
	return title + "  " + text.key(clitext.HealthCompactStack, clitext.Params{clitext.ParamStack: stack})
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
			counts = append(counts, Count{State: state, Text: termpaint.Escape(text.key(summaryKey(state), clitext.Params{clitext.ParamCount: count}))})
		}
	}
	ok := summary.Count(contracts.HealthOK)
	return append(counts, Count{State: contracts.HealthOK, Text: termpaint.Escape(text.key(clitext.HealthSummaryOK, clitext.Params{clitext.ParamCount: ok}))})
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
	case check.Facts.Forge != nil:
		return forgeFacts(check.Facts.Forge, text)
	case check.Facts.SelfTools != nil:
		return selfToolsFacts(check.Facts.SelfTools, text)
	case check.Facts.RTK != nil:
		return rtkFacts(check.Facts.RTK, text)
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

func rtkFacts(state *RTK, text *renderer) string {
	switch {
	case !state.Present:
		return text.key(clitext.HealthRTKAbsent, nil)
	case state.UserHook:
		return text.key(clitext.HealthRTKUserHook, clitext.Params{clitext.ParamVersion: state.Version})
	}
	return text.key(clitext.HealthRTKFacts, clitext.Params{clitext.ParamVersion: state.Version})
}

func selfToolsFacts(self *SelfTools, text *renderer) string {
	if !self.Enabled {
		return text.key(clitext.HealthSelfToolsOff, nil)
	}
	return text.key(clitext.HealthSelfToolsFacts, clitext.Params{
		clitext.ParamMode:  self.Exposure,
		clitext.ParamCount: self.Tools,
		clitext.ParamSize:  self.SchemaBytes,
	})
}

func compactionFacts(facts Facts, text *renderer) string {
	segments := []string{compactionLastFact(facts, text)}
	if key := facts.CompactionKey; key != nil {
		segments = append(segments, compactionKeyFact(key, text))
	}
	if reply := facts.CompactionSucceeded; reply != nil {
		segments = append(segments, text.key(clitext.HealthCompactReply, clitext.Params{
			clitext.ParamOutcome: text.outcome(reply.Outcome),
			clitext.ParamDate:    compactionDate(reply.At),
		}))
	}
	if failure := facts.CompactionFailed; failure != nil {
		segments = append(segments, text.key(clitext.HealthCompactError, clitext.Params{
			clitext.ParamDate: compactionDate(failure.At),
			clitext.ParamNote: compactionNote(failure, text),
		}))
	}
	if facts.CompactionPartial {
		segments = append(segments, text.key(clitext.HealthCompactPartial, nil))
	}
	return strings.Join(segments, factsSep)
}

func compactionLastFact(facts Facts, text *renderer) string {
	if last := facts.Compaction; last != nil {
		return text.key(clitext.HealthCompactLast, clitext.Params{
			clitext.ParamOutcome: text.outcome(last.Outcome),
			clitext.ParamDate:    compactionDate(last.At),
		})
	}
	if facts.CompactionUnknown {
		return text.key(clitext.HealthCompactUnknown, nil)
	}
	return text.key(clitext.HealthCompactNever, nil)
}

func compactionKeyFact(key *CompactionKey, text *renderer) string {
	if key.Present {
		return text.key(clitext.HealthCompactKeyOn, clitext.Params{clitext.ParamName: key.Env})
	}
	return text.key(clitext.HealthCompactKeyOff, clitext.Params{clitext.ParamName: key.Env})
}

func compactionNote(failure *Compaction, text *renderer) string {
	if failure.Key != "" {
		return text.key(failure.Key, nil)
	}
	return text.key(clitext.HealthCompactNoNote, nil)
}

func compactionDate(at time.Time) string {
	return at.Local().Format(time.DateTime)
}

func (r *renderer) outcome(outcome contracts.CompactOutcome) string {
	key, known := clitext.CompactOutcomeKey(outcome)
	if !known {
		return outcome.String()
	}
	return r.key(key, nil)
}

func causeText(check Check, report contracts.ProbeReport, text *renderer) string {
	cause := detailText(report, check.Facts.Forge, text)
	forge := check.Facts.Forge
	if forge == nil || forge.Login == "" || report.Detail.Code == contracts.CodeHealthSignedInAs {
		return cause
	}
	login := contracts.Notice{Code: contracts.CodeHealthSignedInAs, Args: []contracts.Arg{contracts.Text("Name", forge.Login)}}
	return text.notice(login) + factsSep + cause
}

func detailText(report contracts.ProbeReport, forge *Forge, text *renderer) string {
	if forge == nil || forge.RepoErr == "" || report.Detail.Code == contracts.CodeHealthRepoAccessUnreadable {
		return text.notice(report.Detail)
	}
	repo := text.key(clitext.HealthForgeRepoUnreadable, clitext.Params{clitext.ParamReason: forge.RepoErr})
	return text.notice(report.Detail) + factsSep + repo
}

func forgeFacts(forge *Forge, text *renderer) string {
	segments := []string{text.key(clitext.HealthForgeSource, clitext.Params{clitext.ParamSource: text.key(clitext.ForgeSourceKey(forge.Source), nil)})}
	if forge.Quota != nil {
		segments = append(segments, text.key(clitext.HealthForgeQuota, clitext.Params{clitext.ParamCount: forge.Quota.Remaining}))
		if !forge.Quota.Reset.IsZero() {
			segments = append(segments, text.key(clitext.HealthForgeReset, clitext.Params{clitext.ParamTime: forge.Quota.Reset.Local().Format(time.TimeOnly)}))
		}
	}
	if forge.Repo != "" {
		segments = append(segments, forgeRepoFact(forge, text))
	}
	return strings.Join(segments, factsSep)
}

func forgeRepoFact(forge *Forge, text *renderer) string {
	if forge.Permission == nil {
		return text.key(clitext.HealthForgeRepoUnknown, clitext.Params{clitext.ParamRepo: forge.Repo})
	}
	permission := text.key(clitext.ForgePermissionKey(*forge.Permission), nil)
	return text.key(clitext.HealthForgeRepo, clitext.Params{clitext.ParamRepo: forge.Repo, clitext.ParamPermission: permission})
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
