package tui

import "testing"

func TestSupportsKitty(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"kitty via TERM", map[string]string{"TERM": "xterm-kitty"}, true},
		{"ghostty via TERM_PROGRAM", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "ghostty"}, true},
		{"wezterm via TERM_PROGRAM", map[string]string{"TERM_PROGRAM": "WezTerm"}, true},
		{"plain xterm", map[string]string{"TERM": "xterm-256color"}, false},
		{"empty env", map[string]string{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := func(k string) string { return c.env[k] }
			if got := supportsKitty(env); got != c.want {
				t.Errorf("supportsKitty(%v) = %v, want %v", c.env, got, c.want)
			}
		})
	}
}
