package manage

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

type promptDecider struct {
	in *bufio.Reader
	s  style
}

func newPromptDecider(in *bufio.Reader, s style) decider { return promptDecider{in: in, s: s} }

func (p promptDecider) Warn(_ context.Context, findings []string) Decision {
	_, _ = fmt.Fprintf(os.Stderr, "\n  %s\n", p.s.wrap(p.s.bold, "before writing"))
	if len(findings) == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "    %s nothing known against this change\n", p.s.wrap(p.s.dim, "·"))
	}
	for _, f := range findings {
		_, _ = fmt.Fprintf(os.Stderr, "    %s %s\n", p.s.wrap(p.s.red, "!"), f)
	}
	if p.yes("proceed?", true) {
		return Proceed
	}
	return Abort
}

func (p promptDecider) Failed(_ context.Context, buildOutput string) Decision {
	_, _ = fmt.Fprintf(os.Stderr, "\n  %s\n", p.s.wrap(p.s.bold+p.s.red, "the composition does not build"))
	for _, line := range strings.Split(strings.TrimRight(buildOutput, "\n"), "\n") {
		_, _ = fmt.Fprintf(os.Stderr, "    %s\n", p.s.wrap(p.s.dim, line))
	}
	if p.yes("restore the tree as it was?", true) {
		return Restore
	}
	return Keep
}

func (p promptDecider) yes(question string, def bool) bool {
	hint := "[Y/n]"
	if !def {
		hint = "[y/N]"
	}
	ans := strings.ToLower(promptLine(p.in, fmt.Sprintf("  %s %s ", question, p.s.wrap(p.s.dim, hint))))
	switch ans {
	case "":
		return def
	case "y", "yes":
		return true
	default:
		return false
	}
}
