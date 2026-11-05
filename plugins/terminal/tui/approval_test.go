package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const testApprovalID = "7"

func testApprovalValue(role string) string {
	return "herrscher-approval:" + testApprovalID + ":" + role
}

func approvalMenuEvent(t *testing.T, command string, roles ...string) contracts.Event {
	t.Helper()
	menu := approvalMenu{Prompt: "allow Bash(" + command + ")? (answer with `approve allow " + testApprovalID + "`)"}
	for _, role := range roles {
		menu.Options = append(menu.Options, approvalMenuOption{Label: role, Value: testApprovalValue(role)})
	}
	encoded, err := json.Marshal(menu)
	if err != nil {
		t.Fatal(err)
	}
	return contracts.Event{T: eventMenu, Text: menu.Prompt, Value: string(encoded)}
}

func newApprovalModel(t *testing.T, width int, command string, roles ...string) (*model, *fakeBackend) {
	t.Helper()
	fb := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "work", ChannelID: "a", Project: "herrscher", Dir: "/home/shan/dev/wt/tui"}}}
	m := newModel(fb)
	m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: approvalMenuEvent(t, command, roles...)})
	m.active = "a"
	return m, fb
}

func allApprovalRoles() []string {
	return []string{approvalAllow, approvalAllowSession, approvalAllowAlways, approvalDeny}
}

func sendKey(m *model, key tea.KeyMsg) {
	m.Update(key)
}

func pressRunes(m *model, text string) {
	sendKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

func TestApprovalMenuOpensBoxAboveComposer(t *testing.T) {
	m, _ := newApprovalModel(t, 120, "go test ./plugins/terminal/tui/", allApprovalRoles()...)
	view := m.View()
	for _, want := range []string{"! Approval", "asked by the host", "go test ./plugins/terminal/tui/", "Once", "For the session", "Always", "Deny with a reason", "deny in 5:00"} {
		if !strings.Contains(view, want) {
			t.Fatalf("approval box misses %q:\n%s", want, view)
		}
	}
	if !m.tabs["a"].pendingApproval {
		t.Fatal("tab must report a pending approval")
	}
}

func TestApprovalKeysSendExactValues(t *testing.T) {
	cases := []struct {
		key  string
		role string
	}{
		{"o", approvalAllow},
		{"s", approvalAllowSession},
		{"t", approvalAllowAlways},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			m, fb := newApprovalModel(t, 120, "git push", allApprovalRoles()...)
			pressRunes(m, tc.key)
			if len(fb.picks) != 1 {
				t.Fatalf("picks = %+v, want one", fb.picks)
			}
			if got, want := fb.picks[0], (pickedChoice{channel: "a", value: testApprovalValue(tc.role)}); got != want {
				t.Fatalf("pick = %+v, want %+v", got, want)
			}
			if m.tabs["a"].pendingApproval {
				t.Fatal("answered approval must clear the pending flag")
			}
		})
	}
}

func TestApprovalDenyWithReasonSendsTheReason(t *testing.T) {
	m, fb := newApprovalModel(t, 120, "git push", allApprovalRoles()...)
	pressRunes(m, "r")
	if !strings.Contains(m.View(), "reason") {
		t.Fatal("r must open the reason field")
	}
	pressRunes(m, "pas de push: relis d'abord")
	sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	want := testApprovalValue(approvalDeny) + ":pas de push: relis d'abord"
	if len(fb.picks) != 1 || fb.picks[0].value != want {
		t.Fatalf("picks = %+v, want value %q", fb.picks, want)
	}
}

func TestApprovalDenyWithEmptyReasonSendsPlainDeny(t *testing.T) {
	m, fb := newApprovalModel(t, 120, "git push", allApprovalRoles()...)
	pressRunes(m, "r")
	sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(fb.picks) != 1 || fb.picks[0].value != testApprovalValue(approvalDeny) {
		t.Fatalf("picks = %+v, want a plain deny", fb.picks)
	}
}

func TestApprovalEscLeavesTheReasonField(t *testing.T) {
	m, fb := newApprovalModel(t, 120, "git push", allApprovalRoles()...)
	pressRunes(m, "r")
	sendKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if len(fb.picks) != 0 || !m.tabs["a"].pendingApproval {
		t.Fatalf("esc in the reason field must neither answer nor clear the approval, picks=%+v", fb.picks)
	}
}

func TestApprovalArrowsAndEnterChooseTheRow(t *testing.T) {
	m, fb := newApprovalModel(t, 120, "git push", allApprovalRoles()...)
	sendKey(m, tea.KeyMsg{Type: tea.KeyDown})
	sendKey(m, tea.KeyMsg{Type: tea.KeyDown})
	sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(fb.picks) != 1 || fb.picks[0].value != testApprovalValue(approvalAllowAlways) {
		t.Fatalf("picks = %+v, want the allow-always value", fb.picks)
	}
}

func TestApprovalOffersOnlyTheRolesTheHostSent(t *testing.T) {
	m, fb := newApprovalModel(t, 120, "git push", approvalAllow, approvalDeny)
	view := m.View()
	if strings.Contains(view, "Always") || strings.Contains(view, "For the session") {
		t.Fatalf("roles not offered by the host must not appear:\n%s", view)
	}
	pressRunes(m, "s")
	if len(fb.picks) != 0 {
		t.Fatalf("a key for a role not offered must not answer, picks=%+v", fb.picks)
	}
	if m.input.Value() != "s" {
		t.Fatalf("an unbound letter must reach the composer, got %q", m.input.Value())
	}
}

func TestApprovalLetterKeysYieldToTypingInTheComposer(t *testing.T) {
	m, fb := newApprovalModel(t, 120, "git push", allApprovalRoles()...)
	m.input.SetValue("bonjour")
	pressRunes(m, "o")
	if len(fb.picks) != 0 {
		t.Fatalf("typing into a non-empty composer must not answer, picks=%+v", fb.picks)
	}
}

func TestApprovalCountdownJumpsToFiveSecondsWithX(t *testing.T) {
	m, _ := newApprovalModel(t, 120, "git push", allApprovalRoles()...)
	pressRunes(m, "x")
	if !strings.Contains(m.View(), "deny in 0:05") {
		t.Fatalf("x must jump the countdown to 0:05:\n%s", m.View())
	}
}

func TestApprovalRefusedAtZero(t *testing.T) {
	m, fb := newApprovalModel(t, 120, "git push", allApprovalRoles()...)
	m.tabs["a"].approval.deadline = time.Now().Add(-time.Second)
	m.Update(tickMsg{})
	if len(fb.picks) != 1 || fb.picks[0].value != testApprovalValue(approvalDeny) {
		t.Fatalf("picks = %+v, want a refusal at zero", fb.picks)
	}
	if m.tabs["a"].pendingApproval {
		t.Fatal("a refused approval must clear the pending flag")
	}
}

func TestApprovalNotExpiredBeforeZero(t *testing.T) {
	m, fb := newApprovalModel(t, 120, "git push", allApprovalRoles()...)
	m.Update(tickMsg{})
	if len(fb.picks) != 0 || !m.tabs["a"].pendingApproval {
		t.Fatalf("a fresh approval must wait, picks=%+v", fb.picks)
	}
}

func TestApprovalMenuWithoutApprovalRolesStaysStatusText(t *testing.T) {
	fb := &fakeBackend{}
	m := newModel(fb)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: eventMenu, Text: "pick: first"}})
	if m.tabs["a"].pendingApproval {
		t.Fatal("a menu without approval roles must not open the approval box")
	}
}

func TestApprovalRendersAtEveryWidth(t *testing.T) {
	long := "go test " + strings.Repeat("./plugins/terminal/tui/some/deep/path ", 6) + "END"
	for _, width := range []int{60, 80, 120, 200} {
		t.Run(fmt.Sprintf("%dcols", width), func(t *testing.T) {
			m, _ := newApprovalModel(t, width, long, allApprovalRoles()...)
			checkApprovalFits(t, m.View(), width)
		})
	}
}

func checkApprovalFits(t *testing.T, view string, width int) {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("width %d: line of %d columns overflows: %q", width, w, line)
		}
	}
	for _, want := range []string{"! Approval", "deny in 5:00", "Once", "END"} {
		if !strings.Contains(view, want) {
			t.Fatalf("width %d misses %q:\n%s", width, want, view)
		}
	}
	if strings.Count(view, "…") == 0 {
		t.Fatalf("width %d: the long command must be middle-truncated with …", width)
	}
}

func TestApprovalWideLayoutKeepsNotesOnTheSameRow(t *testing.T) {
	m, _ := newApprovalModel(t, 120, "git push", allApprovalRoles()...)
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "Once") && !strings.Contains(line, "only this command") {
			t.Fatalf("at 120 columns the note belongs on the choice row: %q", line)
		}
	}
}

func TestApprovalNarrowLayoutPutsNotesBelowTheChoice(t *testing.T) {
	m, _ := newApprovalModel(t, 60, "git push", allApprovalRoles()...)
	view := m.View()
	if !strings.Contains(view, "only this command") {
		t.Fatalf("narrow layout must still show the note:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Once") && strings.Contains(line, "only this command") {
			t.Fatalf("under 80 columns each choice takes two rows: %q", line)
		}
	}
}

func TestMiddleTruncateKeepsBothEnds(t *testing.T) {
	got := middleTruncate("abcdefghijklmnopqrstuvwxyz", 11, "…")
	if lipgloss.Width(got) > 11 || !strings.HasPrefix(got, "abcd") || !strings.HasSuffix(got, "wxyz") || !strings.Contains(got, "…") {
		t.Fatalf("middleTruncate = %q", got)
	}
}

func TestApprovalEventSetsTheRealDeadline(t *testing.T) {
	m, _ := newApprovalModel(t, 120, "git push", allApprovalRoles()...)
	due := time.Now().Add(42 * time.Second).UTC().Format(time.RFC3339)
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: eventApproval, Value: testApprovalID, Text: due}})
	if view := m.View(); !strings.Contains(view, "deny in 0:4") {
		t.Fatalf("the host deadline must drive the countdown:\n%s", view)
	}
}

func TestApprovalAlwaysShowsTheExactRule(t *testing.T) {
	fb := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "work", ChannelID: "a"}}}
	m := newModel(fb)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	menu := approvalMenu{Prompt: "allow Bash(git push)?", Options: []approvalMenuOption{
		{Label: approvalAllow, Value: testApprovalValue(approvalAllow)},
		{Label: "bash:git push*", Value: testApprovalValue(approvalAllowAlways)},
	}}
	encoded, err := json.Marshal(menu)
	if err != nil {
		t.Fatal(err)
	}
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: eventMenu, Value: string(encoded)}})
	m.active = "a"
	if !strings.Contains(m.View(), "bash:git push*") {
		t.Fatalf("allow-always must show the rule the host will write:\n%s", m.View())
	}
}

func TestApprovalDeadlineArrivingBeforeTheMenuApplies(t *testing.T) {
	fb := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "work", ChannelID: "a"}}}
	m := newModel(fb)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	due := time.Now().Add(42 * time.Second).UTC().Format(time.RFC3339)
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: eventApproval, Value: testApprovalID, Text: due}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: approvalMenuEvent(t, "git push", allApprovalRoles()...)})
	m.active = "a"
	if view := m.View(); !strings.Contains(view, "deny in 0:4") {
		t.Fatalf("an approval event sent before the menu must drive the countdown:\n%s", view)
	}
}

func TestApprovalDeadlineExpiresWhenTheMenuNeverComes(t *testing.T) {
	fb := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "work", ChannelID: "a"}}}
	m := newModel(fb)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	due := time.Now().Add(42 * time.Second).UTC().Format(time.RFC3339)
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: eventApproval, Value: testApprovalID, Text: due}})
	m.expireApprovals(time.Now().Add(approvalEarlyTTL + time.Second))
	if _, ok := m.approvalDues[testApprovalID]; ok {
		t.Fatal("an unmatched approval deadline must be forgotten after its retention")
	}
}

func TestApprovalShowsTheJudgeRisk(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	menu := approvalMenu{Prompt: "allow Bash(go test ./...)? (answer with `approve allow " + testApprovalID + "`)", Risk: riskHighName}
	for _, role := range []string{approvalAllow, approvalDeny} {
		menu.Options = append(menu.Options, approvalMenuOption{Label: role, Value: testApprovalValue(role)})
	}
	encoded, err := json.Marshal(menu)
	if err != nil {
		t.Fatal(err)
	}
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c1"}, Event: contracts.Event{T: eventMenu, Text: menu.Prompt, Value: string(encoded)}})
	if !strings.Contains(strings.Join(m.approvalLines(time.Now(), unboundedRows), "\n"), "high risk") {
		t.Errorf("the approval must show the judge risk when the host sends one")
	}
}

func TestApprovalWithoutRiskHasNoJudgeLine(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c1"}, Event: approvalMenuEvent(t, "go test ./...", allApprovalRoles()...)})
	if strings.Contains(strings.Join(m.approvalLines(time.Now(), unboundedRows), "\n"), "judged") {
		t.Errorf("the approval must not show a judge line without a risk")
	}
}

func TestApprovalRiskSentBeforeTheMenuIsShown(t *testing.T) {
	fb := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "work", ChannelID: "a"}}}
	m := newModel(fb)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: eventApprovalRisk, Value: testApprovalID, Text: riskHighName}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: approvalMenuEvent(t, "git push", allApprovalRoles()...)})
	m.active = "a"
	if view := m.View(); !strings.Contains(view, "high risk") {
		t.Fatalf("a risk event sent before the menu must show the judge line:\n%s", view)
	}
}
