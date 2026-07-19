package manager

import (
	"slices"
	"testing"
)

func TestParseGateways(t *testing.T) {
	// def is the injected primary default used to prove an empty/invalid list
	// falls back to what the composition root supplies (never a hard-coded kind).
	def := []string{"chat"}
	cases := []struct {
		name         string
		list         string
		terminalOnly bool
		defaults     []string
		want         []string
	}{
		{"default falls back to injected", "", false, def, []string{"chat"}},
		{"terminal only flag", "", true, def, []string{"terminal"}},
		{"explicit list", "chat,terminal", false, def, []string{"chat", "terminal"}},
		{"trims and drops empties", " chat , , terminal ", false, def, []string{"chat", "terminal"}},
		{"dedups", "chat,chat", false, def, []string{"chat"}},
		{"flag wins over empty list only", "chat,terminal", true, def, []string{"chat", "terminal"}},
		{"bare comma defaults", ",", false, def, []string{"chat"}},
		{"all empty separators default", " , , ", false, def, []string{"chat"}},
		{"case insensitive", "Chat", false, def, []string{"chat"}},
		{"drops invalid path entry", "../chat,terminal", false, def, []string{"terminal"}},
		{"drops invalid charset", "bad kind!,chat", false, def, []string{"chat"}},
		{"all invalid defaults", "bad kind!", false, def, []string{"chat"}},
		{"empty defaults yield nothing", "", false, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseGateways(tc.list, tc.terminalOnly, tc.defaults)
			if !slices.Equal(got, tc.want) {
				t.Errorf("ParseGateways(%q,%v,%v) = %v, want %v", tc.list, tc.terminalOnly, tc.defaults, got, tc.want)
			}
		})
	}
}
