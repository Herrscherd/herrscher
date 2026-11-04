package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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
	for _, want := range []string{"Diff du tour", "a.txt", "+2 −1", "b.txt", "relus 0/2"} {
		if !strings.Contains(view, want) {
			t.Fatalf("panel misses %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "-0") {
		t.Fatalf("a count without deletions must not show -0:\n%s", view)
	}
}

func TestListRowsDropTheStatusLetterAndIndentByThree(t *testing.T) {
	dir := initRepo(t)
	writeAndCommitBase(t, dir)
	m, _ := newDiffModel(t, dir, nil, 120, 30)
	press(m, "ctrl+d")

	row := m.diffFileRow(1, diffListWidth)
	if !strings.HasPrefix(row, "   ") || strings.Contains(row, "?") || strings.Contains(row, " M ") {
		t.Fatalf("unselected row %q must start with three spaces and carry no status letter", row)
	}
}

func TestListDropsTheFolderTheFilesShare(t *testing.T) {
	dir := initRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkg", "tui"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, dir, "pkg/tui/one.go", "package tui\n")
	writeRepoFile(t, dir, "pkg/tui/two.go", "package tui\n")
	m, _ := newDiffModel(t, dir, nil, 120, 30)
	press(m, "ctrl+d")

	row := m.diffFileRow(0, diffListWidth)
	if strings.Contains(row, "pkg/tui/") || !strings.Contains(row, "one.go") {
		t.Fatalf("list row %q must show the file under the shared folder dropped", row)
	}
	if !strings.Contains(screen(m), "pkg/tui/one.go") {
		t.Fatalf("the pane keeps the full path:\n%s", screen(m))
	}
}

func TestPaneHeaderSaysOldAndNewAtEachWidth(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		t.Run(fmt.Sprintf("%dcols", width), func(t *testing.T) {
			dir := initRepo(t)
			writeAndCommitBase(t, dir)
			m, _ := newDiffModel(t, dir, nil, width, 20)
			press(m, "ctrl+d")

			if !strings.Contains(screen(m), "ancien · nouveau") {
				t.Fatalf("the pane header must read ancien · nouveau:\n%s", screen(m))
			}
		})
	}
}

func TestFooterFoldsWholeHintsAtNarrowWidths(t *testing.T) {
	hints := []string{"⌃D fermer le panneau", "↑↓ fichier", "espace relu", "i consigne", "c commit", "p PR", "b tour ou branche"}
	for _, size := range []struct{ width, height int }{{60, 20}, {80, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			dir := initRepo(t)
			writeAndCommitBase(t, dir)
			m, _ := newDiffModel(t, dir, nil, size.width, size.height)
			press(m, "ctrl+d")

			lines := strings.Split(screen(m), "\n")
			footer := strings.TrimRight(lines[len(lines)-2], " ")
			if !strings.Contains(footer, "⌃D fermer le panneau") {
				t.Fatalf("the close hint must stay on the footer: %q", footer)
			}
			if strings.HasSuffix(footer, "…") {
				return
			}
			for count := 1; count <= len(hints); count++ {
				if footer == " "+strings.Join(hints[:count], " · ") {
					return
				}
			}
			t.Fatalf("footer %q cuts a hint without an ellipsis", footer)
		})
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

func TestDiffScreenRulesTheFooterAndEndsOnTheStatusBar(t *testing.T) {
	for _, size := range []struct{ width, height int }{{60, 20}, {80, 30}, {120, 30}, {200, 30}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			dir := initRepo(t)
			writeAndCommitBase(t, dir)
			m, _ := newDiffModel(t, dir, nil, size.width, size.height)
			press(m, "ctrl+d")

			view := screen(m)
			assertPanelFits(t, view, size.width, size.height)
			lines := strings.Split(view, "\n")
			rule := ansi.Strip(lines[len(lines)-3])
			if rule == "" || strings.Trim(rule, "─") != "" {
				t.Fatalf("a rule must sit above the key hints, got %q", rule)
			}
			status := strings.TrimRight(lines[len(lines)-1], " ")
			if want := strings.TrimRight(m.statusZone()[0], " "); status != want {
				t.Fatalf("the last line must be the shared status bar\nwant %q\ngot  %q", want, status)
			}
		})
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

func TestDiffFileRowKeepsTheFileNameAndTheStats(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.diff.report = diffReport{Files: []diffFile{{Path: "plugins/terminal/tui/composer_queue.go", Added: 38}}}
	row := m.diffFileRow(0, diffListWidth)
	if lipgloss.Width(row) != diffListWidth {
		t.Errorf("row is %d cells wide, want %d", lipgloss.Width(row), diffListWidth)
	}
	for _, want := range []string{"composer_queue.go", "+38"} {
		if !strings.Contains(row, want) {
			t.Errorf("row %q must contain %q", row, want)
		}
	}
	if strings.Contains(row, "-0") {
		t.Errorf("row %q must not show a deletion count when nothing is deleted", row)
	}
}
