package health_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/health"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

func assertNoRawControl(t *testing.T, out string) {
	t.Helper()
	for _, r := range out {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("output carries the raw control %U:\n%q", r, out)
		}
	}
}

func TestTableNeutralisesExternalTextOfEveryCell(t *testing.T) {
	snapshot := health.Snapshot{
		Daemon: health.Daemon{Socket: "/run/herrscher/\x1b.sock"},
		Checks: []health.Check{
			{Group: health.GroupCore, Name: health.ServiceDaemon, Facts: health.Facts{Socket: "/run/herrscher/\x1b.sock"}, Report: contracts.ProbeReport{State: contracts.HealthOK}},
			{
				Group:   health.GroupBackends,
				Name:    "claude\x1b]0;pwn\a",
				Account: "work\r",
				Report: contracts.ProbeReport{
					State:  contracts.HealthDown,
					Detail: contracts.Notice{Code: contracts.CodeHealthUnreachable, Args: []contracts.Arg{contracts.Text("Reason", "boom\x1b[31m\nline2\u009b")}},
				},
			},
		},
	}
	var out bytes.Buffer
	if err := snapshot.Table(&out, clitext.NewMessages(), i18n.English, termpaint.Palette{}); err != nil {
		t.Fatalf("Table() error = %v", err)
	}
	assertNoRawControl(t, out.String())
	for _, want := range []string{`claude\x1b]0;pwn\a`, `work\r`, `boom\x1b[31m\nline2\u009b`, `/run/herrscher/\x1b.sock`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("Table() output does not contain %q:\n%s", want, out.String())
		}
	}
}

func TestDetailsNeutralisesCauseAndRemedyOfACheck(t *testing.T) {
	check := health.Check{
		Group:   health.GroupBackends,
		Name:    "claude",
		Account: "work",
		Report: contracts.ProbeReport{
			State:  contracts.HealthOff,
			Detail: contracts.Notice{Code: contracts.CodeHealthUnreachable, Args: []contracts.Arg{contracts.Text("Reason", "\x1b]52;c;ZQ==\a")}},
			Remedy: contracts.Notice{Code: contracts.CodeHealthRemedyRun, Args: []contracts.Arg{contracts.Text("Command", "herrscher account add\r\x1b[2J")}},
		},
	}
	var out bytes.Buffer
	if err := (health.Snapshot{}).Details(&out, clitext.NewMessages(), []health.Check{check}, i18n.English, termpaint.Palette{}); err != nil {
		t.Fatalf("Details() error = %v", err)
	}
	assertNoRawControl(t, out.String())
	for _, want := range []string{`\x1b]52;c;ZQ==\a`, `herrscher account add\r\x1b[2J`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("Details() output does not contain %q:\n%s", want, out.String())
		}
	}
}

func TestSummaryAndBannerNeutraliseVersionAndWorstCheck(t *testing.T) {
	snapshot := health.Snapshot{
		Daemon: health.Daemon{Version: "v1\x1b[2J"},
		Checks: []health.Check{{Group: health.GroupKeys, Name: "openai\x07", Report: contracts.ProbeReport{State: contracts.HealthDown}}},
	}
	var out bytes.Buffer
	if err := snapshot.Table(&out, clitext.NewMessages(), i18n.English, termpaint.Palette{}); err != nil {
		t.Fatalf("Table() error = %v", err)
	}
	assertNoRawControl(t, out.String())
	for _, want := range []string{`v1\x1b[2J`, `details: herrscher health openai\a`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("Table() output does not contain %q:\n%s", want, out.String())
		}
	}
}

func TestCompactionTitleNeutralisesTheStack(t *testing.T) {
	snapshot := health.Snapshot{
		CompactStack: []string{"jev\x1b[2J"},
		Checks: []health.Check{
			{Group: health.GroupCompaction, Name: "compact", Report: contracts.ProbeReport{State: contracts.HealthOK}},
		},
	}
	view, err := snapshot.View(clitext.NewMessages(), i18n.English)
	if err != nil {
		t.Fatalf("View() error = %v", err)
	}
	var title string
	for _, section := range view.Sections {
		if section.Group == health.GroupCompaction {
			title = section.Title.String()
		}
	}
	assertNoRawControl(t, title)
	if want := `jev\x1b[2J`; !strings.Contains(title, want) {
		t.Errorf("compaction title does not contain %q: %q", want, title)
	}
}

func TestTableEscapesAnEscapeHeldByAnUnknownKindArgument(t *testing.T) {
	code, err := json.Marshal(contracts.CodeHealthUnreachable)
	if err != nil {
		t.Fatalf("json.Marshal(code) error = %v", err)
	}
	wire := fmt.Sprintf(`{"Code":%s,"Args":[{"name":"Reason","kind":99,"value":"\u001b[31mboom"}]}`, code)
	var detail contracts.Notice
	if err := json.Unmarshal([]byte(wire), &detail); err != nil {
		t.Fatalf("json.Unmarshal(notice) error = %v", err)
	}
	snapshot := health.Snapshot{Checks: []health.Check{
		{Group: health.GroupBackends, Name: "claude", Report: contracts.ProbeReport{State: contracts.HealthDown, Detail: detail}},
	}}
	var out bytes.Buffer
	if err := snapshot.Table(&out, clitext.NewMessages(), i18n.English, termpaint.Palette{}); err != nil {
		t.Fatalf("Table() error = %v", err)
	}
	assertNoRawControl(t, out.String())
	if want := `\x1b[31mboom`; !strings.Contains(out.String(), want) {
		t.Errorf("Table() does not show the unknown-kind argument as %q:\n%s", want, out.String())
	}
}

func TestValidUTF8ReachesTheTableUnchanged(t *testing.T) {
	snapshot := health.Snapshot{Checks: []health.Check{
		{Group: health.GroupBackends, Name: "démon ✕ 日本", Report: contracts.ProbeReport{State: contracts.HealthOK}},
	}}
	var out bytes.Buffer
	if err := snapshot.Table(&out, clitext.NewMessages(), i18n.English, termpaint.Palette{}); err != nil {
		t.Fatalf("Table() error = %v", err)
	}
	if !strings.Contains(out.String(), "démon ✕ 日本") {
		t.Errorf("Table() output does not keep the UTF-8 name:\n%s", out.String())
	}
}
