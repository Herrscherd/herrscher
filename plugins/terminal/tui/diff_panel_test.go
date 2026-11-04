package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func newDiffModel(t *testing.T, repo string, run commandRunner, width, height int) (*model, *fakeBackend) {
	t.Helper()
	be := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "s", ChannelID: "c1", Dir: repo}}}
	m := newModel(be)
	m.diffRun = run
	m.syncTabs()
	m.active = "c1"
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return m, be
}

func keyFor(name string) tea.KeyMsg {
	switch name {
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func press(m *model, keys ...string) {
	for _, key := range keys {
		_, cmd := m.Update(keyFor(key))
		settle(m, cmd)
	}
}

func settle(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, next := range msg {
			settle(m, next)
		}
	case diffLoadedMsg, diffActionMsg, turnStartMsg, modeResultMsg:
		_, next := m.Update(msg)
		settle(m, next)
	}
}

func screen(m *model) string {
	return m.View()
}

func writeAndCommitBase(t *testing.T, dir string) {
	t.Helper()
	writeRepoFile(t, dir, "a.txt", "one\nTWO\nthree\n")
	writeRepoFile(t, dir, "b.txt", "new\n")
}

func TestCtrlDOpensTheTurnDiffWithFilesAndCounts(t *testing.T) {
	dir := initRepo(t)
	writeAndCommitBase(t, dir)
	m, _ := newDiffModel(t, dir, nil, 120, 30)

	press(m, "ctrl+d")

	view := screen(m)
	for _, want := range []string{"Diff du tour", "a.txt", "+2 -1", "b.txt", "+1 -0", "relus 0/2"} {
		if !strings.Contains(view, want) {
			t.Fatalf("panel misses %q:\n%s", want, view)
		}
	}
}

func TestEscClosesThePanel(t *testing.T) {
	dir := initRepo(t)
	writeAndCommitBase(t, dir)
	m, _ := newDiffModel(t, dir, nil, 120, 30)
	press(m, "ctrl+d")

	press(m, "esc")

	if strings.Contains(screen(m), "Diff du tour") {
		t.Fatal("esc must close the diff panel")
	}
}

func TestSpaceMarksTheFileReviewedAndMovesOn(t *testing.T) {
	dir := initRepo(t)
	writeAndCommitBase(t, dir)
	m, _ := newDiffModel(t, dir, nil, 120, 30)
	press(m, "ctrl+d")

	press(m, " ")

	view := screen(m)
	if !strings.Contains(view, "relus 1/2") {
		t.Fatalf("le résumé doit compter le fichier relu:\n%s", view)
	}
	if !strings.Contains(view, "+ new") {
		t.Fatalf("selection must move to b.txt, pane shows:\n%s", view)
	}
}

func TestDownArrowShowsTheNextFileDiff(t *testing.T) {
	dir := initRepo(t)
	writeAndCommitBase(t, dir)
	m, _ := newDiffModel(t, dir, nil, 120, 30)
	press(m, "ctrl+d")
	if !strings.Contains(screen(m), "+ TWO") {
		t.Fatalf("first file pane must show its change:\n%s", screen(m))
	}

	press(m, "down")

	if !strings.Contains(screen(m), "+ new") {
		t.Fatalf("down must show b.txt:\n%s", screen(m))
	}
}

func TestNoteIsSentToTheAgentPrefixedByThePath(t *testing.T) {
	dir := initRepo(t)
	writeAndCommitBase(t, dir)
	m, be := newDiffModel(t, dir, nil, 120, 30)
	press(m, "ctrl+d")

	press(m, "i", "f", "a", "i", "s", " ", "m", "i", "e", "u", "x", "enter")

	if len(be.submitted) != 1 {
		t.Fatalf("submitted = %+v, want one note", be.submitted)
	}
	if got := be.submitted[0]; got.channel != "c1" || got.text != "a.txt: fais mieux" {
		t.Fatalf("note = %+v", got)
	}
}

func TestNoteStaysEditableUntilSent(t *testing.T) {
	dir := initRepo(t)
	writeAndCommitBase(t, dir)
	m, be := newDiffModel(t, dir, nil, 120, 30)
	press(m, "ctrl+d", "i", "x", "backspace", "esc")

	if len(be.submitted) != 0 {
		t.Fatalf("esc must not send: %+v", be.submitted)
	}
	if strings.Contains(screen(m), "envoyer à") {
		t.Fatal("esc must close the note box")
	}
}

func TestCommitShowsAnEditableDraftAndEnterCommits(t *testing.T) {
	dir := initRepo(t)
	writeAndCommitBase(t, dir)
	m, _ := newDiffModel(t, dir, nil, 120, 30)
	press(m, "ctrl+d", "c")

	if !strings.Contains(screen(m), "feat: modifier a.txt, b.txt") {
		t.Fatalf("commit box misses the draft:\n%s", screen(m))
	}

	press(m, "e", "!", "enter")

	message := strings.TrimSpace(gitIn(t, dir, "log", "-1", "--format=%B"))
	if !strings.HasPrefix(message, "feat: modifier a.txt, b.txt\n") || !strings.HasSuffix(message, "(+1 -0)!") {
		t.Fatalf("committed message = %q, want the edited draft", message)
	}
	if status := strings.TrimSpace(gitIn(t, dir, "status", "--porcelain")); status != "" {
		t.Fatalf("worktree not clean after commit: %q", status)
	}
}

func TestCommitWithNothingToCommitShowsANotice(t *testing.T) {
	dir := initRepo(t)
	m, _ := newDiffModel(t, dir, nil, 120, 30)
	press(m, "ctrl+d", "c", "enter")

	if !strings.Contains(screen(m), "rien à commiter dans cette portée") {
		t.Fatalf("notice missing:\n%s", screen(m))
	}
}

func TestPRCommitsThenOpensAndShowsTheNumber(t *testing.T) {
	dir := initRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeAndCommitBase(t, dir)
	runner := &scriptedRunner{prURL: "https://github.com/o/r/pull/42\n"}
	m, _ := newDiffModel(t, dir, runner.run, 120, 30)
	press(m, "ctrl+d")

	press(m, "p")

	if !strings.Contains(screen(m), "PR #42 ouverte contre master") {
		t.Fatalf("PR number missing:\n%s", screen(m))
	}
	if len(runner.ghCalls) != 1 {
		t.Fatalf("gh calls = %v, want one pr create", runner.ghCalls)
	}
}

func TestBranchToggleShowsCommitsAheadOfBase(t *testing.T) {
	dir := initRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeRepoFile(t, dir, "a.txt", "one\nTWO\n")
	gitIn(t, dir, "commit", "-q", "-am", "feat: change a")
	m, _ := newDiffModel(t, dir, nil, 120, 30)
	press(m, "ctrl+d")

	press(m, "b")

	view := screen(m)
	if !strings.Contains(view, "branche : 1 commits de plus sur master") {
		t.Fatalf("branch footer missing:\n%s", view)
	}
	if !strings.Contains(view, "a.txt") {
		t.Fatalf("branch scope must list a.txt:\n%s", view)
	}
}

func TestTurnScopeIgnoresChangesMadeBeforeTheTurn(t *testing.T) {
	dir := initRepo(t)
	writeRepoFile(t, dir, "a.txt", "one\nTWO\n")
	m, _ := newDiffModel(t, dir, nil, 120, 30)
	m.input.SetValue("go")
	settle(m, m.handleEnter())
	writeRepoFile(t, dir, "b.txt", "during the turn\n")

	press(m, "ctrl+d")

	view := screen(m)
	if !strings.Contains(view, "b.txt") {
		t.Fatalf("turn diff must list b.txt:\n%s", view)
	}
	if strings.Contains(view, "a.txt") {
		t.Fatalf("turn diff must not list the earlier a.txt change:\n%s", view)
	}
}

func TestReviewCommandOpensThePanelInReviewMode(t *testing.T) {
	dir := initRepo(t)
	writeAndCommitBase(t, dir)
	m, _ := newDiffModel(t, dir, nil, 120, 30)
	m.input.SetValue("/review")

	settle(m, m.handleEnter())

	if !m.diff.open || !m.diff.review {
		t.Fatalf("/review must open the panel in review mode: %+v", m.diff.open)
	}
	if !strings.Contains(screen(m), "Revue") {
		t.Fatalf("review title missing:\n%s", screen(m))
	}
}

const (
	nestedDir  = "internal/deep/nested/module"
	nestedFile = "internal/deep/nested/module/a.txt"
)

func initNestedChange(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, nestedDir), 0o700); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, dir, nestedFile, "base\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "add nested")
	writeRepoFile(t, dir, nestedFile, strings.Repeat("ligne très longue ", 12)+"\nTWO\n")
	return dir
}

func assertPanelFits(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Fatalf("rendered %d lines, want %d", len(lines), height)
	}
	for index, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("line %d is %d columns, want at most %d: %q", index, w, width, line)
		}
	}
	if !strings.Contains(view, "TWO") {
		t.Fatalf("the change must stay visible:\n%s", view)
	}
}

func TestPanelFillsTheScreenAndNeverOverflowsAtEachWidth(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		for _, height := range []int{20, 30} {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				m, _ := newDiffModel(t, initNestedChange(t), nil, width, height)
				press(m, "ctrl+d")

				assertPanelFits(t, screen(m), width, height)
			})
		}
	}
}

func TestWideLayoutShowsListAndPaneSideBySide(t *testing.T) {
	dir := initRepo(t)
	writeAndCommitBase(t, dir)
	m, _ := newDiffModel(t, dir, nil, 120, 30)
	press(m, "ctrl+d")

	rows := strings.Split(screen(m), "\n")
	found := false
	for _, row := range rows {
		if strings.Contains(row, "a.txt") && strings.Contains(row, "│") {
			found = true
		}
	}
	if !found {
		t.Fatalf("at 120 columns the file list and the diff share a row:\n%s", screen(m))
	}
}

func TestFolderOutsideGitShowsAClearMessage(t *testing.T) {
	m, _ := newDiffModel(t, t.TempDir(), nil, 120, 30)
	press(m, "ctrl+d")

	if !strings.Contains(screen(m), "aucun dépôt git dans le dossier de cette session") {
		t.Fatalf("a folder outside git must say so:\n%s", screen(m))
	}
}

func collectMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, next := range msg {
			out = append(out, collectMessages(next)...)
		}
		return out
	case nil:
		return nil
	default:
		return []tea.Msg{msg}
	}
}

func TestTurnIsSentOnlyOnceTheSnapshotIsTaken(t *testing.T) {
	dir := initRepo(t)
	writeRepoFile(t, dir, "a.txt", "one\nTWO\n")
	m, be := newDiffModel(t, dir, nil, 120, 30)
	m.input.SetValue("go")

	msgs := collectMessages(m.handleEnter())

	if len(be.submitted) != 0 {
		t.Fatalf("the turn reached the backend before its snapshot: %+v", be.submitted)
	}
	var snapshot turnStartMsg
	found := false
	for _, msg := range msgs {
		if start, ok := msg.(turnStartMsg); ok {
			snapshot, found = start, true
		}
	}
	if !found {
		t.Fatalf("no snapshot message produced: %+v", msgs)
	}
	if got := gitIn(t, dir, "show", snapshot.tree+":a.txt"); got != "one\nTWO\n" {
		t.Fatalf("snapshot holds %q, want the state before the turn", got)
	}
	for _, msg := range msgs {
		m.Update(msg)
	}
	if len(be.submitted) != 1 || be.submitted[0].text != "go" {
		t.Fatalf("submitted = %+v, want the turn once the snapshot landed", be.submitted)
	}
}
