package health_test

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/compactwire"
	"github.com/Herrscherd/herrscher/core/health"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

func TestTableListsEveryCheckUnderItsGroup(t *testing.T) {
	snapshot := health.Snapshot{Checks: []health.Check{
		{Group: health.GroupGateways, Name: "fakegate", Report: contracts.ProbeReport{State: contracts.HealthOK}},
		{Group: health.GroupBackends, Name: "claude", Report: contracts.ProbeReport{State: contracts.HealthDown}},
	}}
	var out bytes.Buffer
	if err := snapshot.Table(&out, clitext.NewMessages(), i18n.English, termpaint.Palette{}); err != nil {
		t.Fatalf("Table() error = %v", err)
	}
	for _, name := range []string{"fakegate", "claude"} {
		if !strings.Contains(out.String(), name) {
			t.Errorf("Table() output does not list %q:\n%s", name, out.String())
		}
	}
}

func TestUnknownArgKindDoesNotHideTheOtherChecks(t *testing.T) {
	unknownKind, err := compactwire.DecodeNotice(`{"code":1,"args":[{"name":"X","kind":9,"value":"zeta"}]}`)
	if err != nil {
		t.Fatalf("DecodeNotice unknown kind: %v", err)
	}
	mismatch, err := compactwire.DecodeNotice(`{"code":7,"args":[{"name":"Count","kind":2,"value":3}]}`)
	if err != nil {
		t.Fatalf("DecodeNotice mismatch: %v", err)
	}
	snapshot := health.Snapshot{Checks: []health.Check{
		{Group: health.GroupBackends, Name: "broken", Report: contracts.ProbeReport{State: contracts.HealthDown, Detail: unknownKind}},
		{Group: health.GroupBackends, Name: "mismatch", Report: contracts.ProbeReport{State: contracts.HealthDown, Detail: mismatch}},
		{Group: health.GroupBackends, Name: "claude", Report: contracts.ProbeReport{State: contracts.HealthOK}},
	}}
	var out bytes.Buffer
	if err := snapshot.Table(&out, clitext.NewMessages(), i18n.English, termpaint.Palette{}); err != nil {
		t.Fatalf("Table() error = %v, want the unrenderable checks shown as a fallback", err)
	}
	if got := lastFieldOfRow(t, out.String(), "broken"); got != "zeta" {
		t.Errorf("broken row ends with %q, want the argument text %q:\n%s", got, "zeta", out.String())
	}
	if got := lastFieldOfRow(t, out.String(), "mismatch"); got != "7" {
		t.Errorf("mismatch row ends with %q, want the numeric code %q:\n%s", got, "7", out.String())
	}
	lastFieldOfRow(t, out.String(), "claude")
}

func lastFieldOfRow(t *testing.T, out, name string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == name {
			return fields[len(fields)-1]
		}
	}
	t.Fatalf("Table() output has no row named %q:\n%s", name, out)
	return ""
}

func TestDetailsShowsTheRemedyOfACheck(t *testing.T) {
	const command = "herrscher account add claude work"
	check := health.Check{
		Group:   health.GroupBackends,
		Name:    "claude",
		Account: "work",
		Report: contracts.ProbeReport{
			State:  contracts.HealthOff,
			Remedy: contracts.Notice{Code: contracts.CodeHealthRemedyRun, Args: []contracts.Arg{contracts.Text("Command", command)}},
		},
	}
	var out bytes.Buffer
	if err := (health.Snapshot{}).Details(&out, clitext.NewMessages(), []health.Check{check}, i18n.English, termpaint.Palette{}); err != nil {
		t.Fatalf("Details() error = %v", err)
	}
	if !strings.Contains(out.String(), command) {
		t.Errorf("Details() output does not carry the remedy %q:\n%s", command, out.String())
	}
}

func TestSummaryCountsEachStateAndNamesTheWorstCheck(t *testing.T) {
	snapshot := health.Snapshot{
		Daemon:   health.Daemon{PID: 42, Version: "v1.2.3", Started: time.Unix(0, 0)},
		ProbedAt: time.Unix(3*3600, 0),
		Checks: []health.Check{
			{Group: health.GroupKeys, Name: "openai", Report: contracts.ProbeReport{State: contracts.HealthDown}},
			{Group: health.GroupGateways, Name: "chat", Report: contracts.ProbeReport{State: contracts.HealthDegraded}},
			{Group: health.GroupGateways, Name: "slack", Report: contracts.ProbeReport{State: contracts.HealthDegraded}},
			{Group: health.GroupBackends, Name: "claude", Report: contracts.ProbeReport{State: contracts.HealthOK}},
		},
	}
	var out bytes.Buffer
	if err := snapshot.Table(&out, clitext.NewMessages(), i18n.English, termpaint.Palette{}); err != nil {
		t.Fatalf("Table() error = %v", err)
	}
	for _, want := range []string{"herrscher v1.2.3", "daemon up 3 h 0 min", "pid 42", "✕ down", "◐ degraded", "● ok", "1 down · 2 degraded · 1 ok", "details: herrscher health openai"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("Table() output does not contain %q:\n%s", want, out.String())
		}
	}
}

var escapeSequence = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestTableAlignsColumnsTheSameWithAndWithoutColors(t *testing.T) {
	snapshot := health.Snapshot{Checks: []health.Check{
		{Group: health.GroupCore, Name: health.ServiceDaemon, Report: contracts.ProbeReport{State: contracts.HealthOK, Latency: 2 * time.Millisecond}},
		{Group: health.GroupBackends, Name: "claude", Account: "work", Default: true, Report: contracts.ProbeReport{State: contracts.HealthOK, Latency: 900 * time.Millisecond}},
		{Group: health.GroupBackends, Name: "codex", Report: contracts.ProbeReport{State: contracts.HealthOff}},
	}}
	var plain, painted bytes.Buffer
	if err := snapshot.Table(&plain, clitext.NewMessages(), i18n.French, termpaint.Palette{}); err != nil {
		t.Fatalf("Table() plain error = %v", err)
	}
	if err := snapshot.Table(&painted, clitext.NewMessages(), i18n.French, termpaint.ANSIPalette()); err != nil {
		t.Fatalf("Table() painted error = %v", err)
	}
	if got := escapeSequence.ReplaceAllString(painted.String(), ""); got != plain.String() {
		t.Errorf("painted Table() without its colors =\n%s\nwant the plain one\n%s", got, plain.String())
	}
	for _, want := range []string{"démon", "2 ms", "claude · work ★", "900 ms", "codex · natif", "○ déconnecté"} {
		if !strings.Contains(plain.String(), want) {
			t.Errorf("Table() output does not contain %q:\n%s", want, plain.String())
		}
	}
}

func TestLatencyWritesMillisecondsThenSecondsWithTheDecimalOfTheLanguage(t *testing.T) {
	cases := []struct {
		lang    i18n.Language
		elapsed time.Duration
		want    string
	}{
		{i18n.French, 142 * time.Millisecond, "142 ms"},
		{i18n.French, 1100 * time.Millisecond, "1,1 s"},
		{i18n.English, 1800 * time.Millisecond, "1.8 s"},
		{i18n.English, 3 * time.Minute, "3 min"},
	}
	for _, tc := range cases {
		got, err := health.Latency(clitext.NewMessages(), tc.lang, tc.elapsed)
		if err != nil || got != tc.want {
			t.Errorf("Latency(%v, %v) = %q, %v, want %q", tc.lang, tc.elapsed, got, err, tc.want)
		}
	}
}

func TestTableShortensTheSocketAndNamesTheCompactionStack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	snapshot := health.Snapshot{
		Daemon:       health.Daemon{Socket: home + "/.herrscher/host.sock"},
		CompactStack: []string{"jev", "backend"},
		Checks: []health.Check{
			{Group: health.GroupCore, Name: health.ServiceDaemon, Report: contracts.ProbeReport{State: contracts.HealthOK}},
			{Group: health.GroupCompaction, Name: "jev", Report: contracts.ProbeReport{State: contracts.HealthOK}},
		},
	}
	var out bytes.Buffer
	if err := snapshot.Table(&out, clitext.NewMessages(), i18n.French, termpaint.Palette{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"~/.herrscher/host.sock", "compactStack = jev → backend"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("table misses %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), home) {
		t.Errorf("table keeps the home path:\n%s", out.String())
	}
}
