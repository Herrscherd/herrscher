package host

import (
	"strings"
	"testing"
)

func TestEnvWrappedCommandName(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{name: "split string argument", cmd: `env -S "claude --x"`, want: "claude"},
		{name: "attached split string", cmd: `env -Sclaude --x`, want: "claude"},
		{name: "split string as separate words", cmd: `env -S claude --x`, want: "claude"},
		{name: "long split string with equals", cmd: `env --split-string="claude --x"`, want: "claude"},
		{name: "split string in a cluster", cmd: `env -iS claude`, want: "claude"},
		{name: "split string with a bare assignment", cmd: `env -S "FOO=1 claude"`, want: "claude"},
		{name: "assignment after end of options", cmd: `env -- FOO=1 claude`, want: "claude"},
		{name: "command right after end of options", cmd: `env -- claude --x`, want: "claude"},
		{name: "dash as an alias of -i", cmd: `env - claude`, want: "claude"},
		{name: "dash alone names nothing", cmd: `env -`, want: ""},
		{name: "ignore environment with assignment", cmd: `env -i FOO=1 claude`, want: "claude"},
		{name: "unset option takes its argument", cmd: `env -u FOO claude`, want: "claude"},
		{name: "split string without argument names nothing", cmd: `env -S`, want: ""},
		{name: "end of options without command names nothing", cmd: `env --`, want: ""},
		{name: "nested env inside a split string", cmd: `env -S "env -u X claude"`, want: "claude"},
		{name: "nested env after end of options", cmd: `env -- env -u X claude`, want: "claude"},
		{name: "env run by env after an option", cmd: `env -i env claude`, want: "claude"},
		{name: "doubly nested split string", cmd: `env -S "env -S 'env -u X claude --x'"`, want: "claude"},
		{name: "nested env with assignment", cmd: `env -S "env FOO=1 claude"`, want: "claude"},
		{name: "nesting within the limit", cmd: strings.Repeat("env ", 5) + "claude", want: "claude"},
		{name: "nesting past the limit names nothing", cmd: strings.Repeat("env ", 12) + "claude", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commandBinary(commandWords(tt.cmd)); got != tt.want {
				t.Fatalf("commandBinary(%q) = %q, want %q", tt.cmd, got, tt.want)
			}
		})
	}
}
