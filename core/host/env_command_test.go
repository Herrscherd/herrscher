package host

import "testing"

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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commandBinary(commandWords(tt.cmd)); got != tt.want {
				t.Fatalf("commandBinary(%q) = %q, want %q", tt.cmd, got, tt.want)
			}
		})
	}
}
