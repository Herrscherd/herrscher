package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// longModel is a ready model whose active tab holds a conversation too long to
// read at a glance — which is the only situation any of this is for.
func longModel(t *testing.T) *model {
	t.Helper()
	m := readyModel(&fakeBackend{})
	tb := m.ensureTab("a")
	m.active = "a"
	for i := 0; i < 12; i++ {
		tb.appendEntry(entry{role: roleYou, text: "question " + string(rune('a'+i))})
		tb.appendEntry(entry{role: roleAgent, text: "answer about widgets"})
	}
	tb.appendEntry(entry{role: roleAgent, text: "the needle is here"})
	m.syncViewport()
	return m
}

func TestSearchFindsEveryMatchAndScrollsToTheFirst(t *testing.T) {
	m := longModel(t)
	m.openSearch()
	m.typeSearch("needle")

	if len(m.searchHits) != 1 {
		t.Fatalf("one line holds the needle, got %d", len(m.searchHits))
	}
	if m.vp.YOffset != m.searchHits[0] && m.vp.YOffset+m.vp.Height <= m.searchHits[0] {
		t.Fatalf("the first hit must be on screen: offset %d, hit %d", m.vp.YOffset, m.searchHits[0])
	}
	if !strings.Contains(m.thinkingContent(), ansiReverse) {
		t.Fatalf("a match must be marked in the transcript")
	}
}

// The search is over what the operator can see, so its matches are the rendered
// lines and its count is the count of them.
func TestSearchCountsEveryMatchingLine(t *testing.T) {
	lines := []string{"alpha", "beta widgets", "gamma", "more WIDGETS here"}
	got := searchHits(lines, "widgets")
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("searchHits = %v, want [1 3] (case-insensitively)", got)
	}
	if n := len(searchHits(lines, "")); n != 0 {
		t.Fatalf("an empty query matches nothing, got %d", n)
	}
}

func TestSearchNextAndPreviousCycleAndWrap(t *testing.T) {
	m := longModel(t)
	m.openSearch()
	m.typeSearch("widgets")
	if len(m.searchHits) < 3 {
		t.Fatalf("the fixture needs several hits, got %d", len(m.searchHits))
	}
	last := len(m.searchHits) - 1

	m.searchStep(-1)
	if m.searchIdx != last {
		t.Fatalf("stepping back from the first hit must wrap to the last, got %d", m.searchIdx)
	}
	m.searchStep(1)
	if m.searchIdx != 0 {
		t.Fatalf("stepping past the last must wrap to the first, got %d", m.searchIdx)
	}
}

// A search is a detour. Closing it puts the operator back where they were, or the
// feature costs them their place every time they use it.
func TestClosingSearchRestoresTheScrollPosition(t *testing.T) {
	m := longModel(t)
	m.vp.SetYOffset(3)
	before := m.vp.YOffset

	m.openSearch()
	m.typeSearch("needle")
	m.closeSearch()

	if m.vp.YOffset != before {
		t.Fatalf("closing must restore the offset: got %d, want %d", m.vp.YOffset, before)
	}
	if m.searchOpen || m.searchQuery != "" {
		t.Fatalf("closing must clear the search: open=%v query=%q", m.searchOpen, m.searchQuery)
	}
}

// Esc closes the overlay rather than interrupting the turn: while the search is
// open it is the search the operator is looking at.
func TestEscClosesTheSearchInsteadOfTheSession(t *testing.T) {
	m := longModel(t)
	m.openSearch()
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.searchOpen {
		t.Fatalf("Esc must close the search overlay")
	}
}

func TestTurnBoundariesAreWhereTurnsStart(t *testing.T) {
	lines := []string{
		glyphCursor + " ask",
		"  body",
		agentTitle + " ─────",
		"  answer",
		glyphCursor + " ask again",
	}
	got := turnBoundaries(lines)
	want := []int{0, 2, 4}
	if len(got) != len(want) {
		t.Fatalf("turnBoundaries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("turnBoundaries = %v, want %v", got, want)
		}
	}
}

func TestJumpToTurnLandsOnABoundary(t *testing.T) {
	m := longModel(t)
	m.vp.GotoTop()
	m.jumpTurn(1)
	bounds := turnBoundaries(strings.Split(m.thinkingContent(), "\n"))
	if len(bounds) < 2 {
		t.Fatalf("the fixture needs several turns, got %v", bounds)
	}
	found := false
	for _, b := range bounds {
		if b == m.vp.YOffset {
			found = true
		}
	}
	if !found {
		t.Fatalf("a jump must land on a boundary: offset %d, boundaries %v", m.vp.YOffset, bounds)
	}
}

// An old turn is context, not reading matter. Collapsed it is one line; expanded
// it is what it was, character for character — a fold that rewrites the
// transcript is a fold nobody dares use.
func TestFoldingOldTurnsCollapsesThemAndRestoresExactly(t *testing.T) {
	lines := []string{
		glyphCursor + " ask",
		"  body one",
		"  body two",
		agentTitle + " ─",
		"  answer",
	}
	folded := foldTurns(lines, 1)
	if len(folded) != 1+len(lines)-3 {
		t.Fatalf("the first turn must collapse to one line: %q", folded)
	}
	if strings.Contains(strings.Join(folded, "\n"), "body two") {
		t.Fatalf("a folded turn must not still show its body: %q", folded)
	}
	if !strings.Contains(folded[0], "ask") {
		t.Fatalf("the summary must still say which turn it is: %q", folded[0])
	}

	// The source is untouched, so unfolding is only a matter of not folding.
	again := foldTurns(lines, 99)
	if strings.Join(again, "\n") != strings.Join(lines, "\n") {
		t.Fatalf("unfolded lines must be identical to the source: %q", again)
	}
}

func TestToggleTurnFoldIsReversible(t *testing.T) {
	m := longModel(t)
	before := m.thinkingContent()
	m.toggleTurnFold()
	if m.thinkingContent() == before {
		t.Fatalf("folding must change what is shown")
	}
	m.toggleTurnFold()
	if m.thinkingContent() != before {
		t.Fatalf("unfolding must restore the transcript exactly")
	}
}

// markLine cuts the original line at offsets found in a lowercased copy. If those
// offsets drift by even one the transcript is corrupted rather than highlighted,
// so what is asserted is that the text comes back exactly as it went in once the
// marks are removed — and that every match got one.
func TestHighlightKeepsTheLineIntactAroundEveryMark(t *testing.T) {
	const line = "Error in Foo and ERROR in bar again"
	got := markLine(line, "error")
	if n := strings.Count(got, ansiReverse); n != 2 {
		t.Fatalf("both matches must be marked, got %d: %q", n, got)
	}
	bare := strings.NewReplacer(ansiReverse, "", ansiReverseOff, "").Replace(got)
	if bare != line {
		t.Fatalf("the line must survive marking unchanged:\n got %q\nwant %q", bare, line)
	}
	// The case of the text is the text's, not the query's.
	if !strings.Contains(got, ansiReverse+"Error"+ansiReverseOff) ||
		!strings.Contains(got, ansiReverse+"ERROR"+ansiReverseOff) {
		t.Fatalf("a match must keep its own case: %q", got)
	}
}

// Folding is a whole-transcript rewrite the cache key cannot see: entry count,
// widths and stream state are all unchanged by a keypress. A cache left valid
// would paint the pre-fold frame and the key would look dead.
func TestFoldKeysRepaintRatherThanServingTheCachedFrame(t *testing.T) {
	long := "```go\n" + strings.Repeat("x := 1\n", foldThreshold+5) + "```"
	for _, c := range []struct {
		name string
		key  tea.KeyType
	}{
		{"code fold", tea.KeyCtrlF},
		{"link selection", tea.KeyCtrlL},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newTestModel()
			tb := m.ensureTab("a")
			m.active = "a"
			tb.appendEntry(entry{role: roleAgent, text: "see https://example.com/a\n\n" + long})
			m.applySize()
			before := m.baseContent()

			m.Update(tea.KeyMsg{Type: c.key})
			if after := m.baseContent(); after == before {
				t.Fatalf("%s must change the painted frame, not serve the cached one", c.name)
			}
		})
	}
}
