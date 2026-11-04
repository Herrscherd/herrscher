package tui

import (
	"reflect"
	"testing"
)

func TestFuzzyFilterKeepsOrderAndDropsMisses(t *testing.T) {
	names := []string{"alice-api", "bob", "ali-docs", "carol"}
	got := fuzzyFilter(names, "ali", func(s string) string { return s })
	want := []string{"alice-api", "ali-docs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fuzzyFilter = %v, want %v", got, want)
	}
}

func TestFuzzyFilterIsCaseInsensitiveAndSubsequence(t *testing.T) {
	names := []string{"Docs-Site", "neublox"}
	got := fuzzyFilter(names, "DS", func(s string) string { return s })
	if !reflect.DeepEqual(got, []string{"Docs-Site"}) {
		t.Fatalf("fuzzyFilter = %v", got)
	}
}

func TestFuzzyFilterEmptyQueryKeepsEverything(t *testing.T) {
	names := []string{"a", "b"}
	if got := fuzzyFilter(names, "  ", func(s string) string { return s }); len(got) != 2 {
		t.Fatalf("empty query must keep all rows: %v", got)
	}
}
