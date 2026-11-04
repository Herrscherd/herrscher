package tui

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestFuzzyMatchIsSubsequenceAndCaseInsensitive(t *testing.T) {
	hit, ok := fuzzyMatch("FB", "foo bar")
	if !ok {
		t.Fatal("fb must match foo bar")
	}
	if !reflect.DeepEqual(hit.positions, []int{0, 4}) {
		t.Fatalf("positions = %v, want [0 4]", hit.positions)
	}
	if _, ok := fuzzyMatch("zz", "foo bar"); ok {
		t.Fatal("zz must not match")
	}
}

func TestFuzzyMatchPrefersConsecutiveAndWordStarts(t *testing.T) {
	tight, _ := fuzzyMatch("abc", "abc")
	loose, _ := fuzzyMatch("abc", "a_x_b_x_c")
	if tight.score <= loose.score {
		t.Fatalf("consecutive match must score higher: tight=%d loose=%d", tight.score, loose.score)
	}
}

func TestRankFuzzyOrdersByScoreStably(t *testing.T) {
	got := rankFuzzy("abc", []string{"a_x_b_x_c", "abc", "zzz", "abc-2"})
	want := []string{"abc", "abc-2", "a_x_b_x_c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rankFuzzy = %v, want %v", got, want)
	}
}

func TestRankFuzzyEmptyQueryKeepsOrder(t *testing.T) {
	got := rankFuzzy("", []string{"b", "a"})
	if !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Fatalf("empty query must keep order: %v", got)
	}
}

func TestHighlightFuzzyKeepsText(t *testing.T) {
	plain := lipgloss.NewStyle()
	hot := lipgloss.NewStyle().Bold(true)
	out := highlightFuzzy("foo bar", []int{0, 4}, plain, hot)
	if ansi.Strip(out) != "foo bar" {
		t.Fatalf("highlight must not change the text: %q", ansi.Strip(out))
	}
}
