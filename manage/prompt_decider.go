package manage

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
)

type promptDecider struct {
	in *bufio.Reader
	s  style
}

func newPromptDecider(in *bufio.Reader, s style) decider { return promptDecider{in: in, s: s} }

func (p promptDecider) Warn(_ context.Context, findings []string) Decision {
	fmt.Fprintf(os.Stderr, "\n  %s\n", p.s.wrap(p.s.bold, msg(clitext.ManageBeforeWriting, nil)))
	if len(findings) == 0 {
		fmt.Fprintf(os.Stderr, "    %s %s\n", p.s.wrap(p.s.dim, "·"), msg(clitext.ManageNothingKnown, nil))
	}
	for _, f := range findings {
		fmt.Fprintf(os.Stderr, "    %s %s\n", p.s.wrap(p.s.red, "!"), f)
	}
	if p.yes(msg(clitext.ManagePromptProceed, nil), true) {
		return Proceed
	}
	return Abort
}

func (p promptDecider) Failed(_ context.Context, buildOutput string) Decision {
	fmt.Fprintf(os.Stderr, "\n  %s\n", p.s.wrap(p.s.bold+p.s.red, msg(clitext.ManageBuildFailed, nil)))
	for _, line := range strings.Split(strings.TrimRight(buildOutput, "\n"), "\n") {
		fmt.Fprintf(os.Stderr, "    %s\n", p.s.wrap(p.s.dim, line))
	}
	if p.yes(msg(clitext.ManagePromptRestore, nil), true) {
		return Restore
	}
	return Keep
}

func (p promptDecider) yes(question string, def bool) bool {
	hint := msg(clitext.ManageHintNoDefault, nil)
	if def {
		hint = msg(clitext.ManageHintYesDefault, nil)
	}
	ans := strings.ToLower(promptLine(p.in, fmt.Sprintf("  %s %s ", question, p.s.wrap(p.s.dim, hint))))
	switch ans {
	case "":
		return def
	case "y", "yes", "o", "oui":
		return true
	default:
		return false
	}
}
