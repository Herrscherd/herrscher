package manage

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

// promptDecider answers the two composition prompts from the terminal. It is the
// CLI half of the Decider seam; the TUI supplies the other, and the transaction
// itself knows about neither.
type promptDecider struct {
	in *bufio.Reader
	s  style
}

// NewPromptDecider returns the terminal Decider, used whenever there is a human
// on stdin to answer.
func NewPromptDecider(in *bufio.Reader, s style) Decider { return promptDecider{in: in, s: s} }

// Warn reports what is known against the change before anything is written. The
// confirmation is asked even when nothing was found, because "nothing known
// against it" is not the same as "it will work".
func (p promptDecider) Warn(ctx context.Context, findings []string) Decision {
	fmt.Fprintf(os.Stderr, "\n  %s\n", p.s.wrap(p.s.bold, "before writing"))
	if len(findings) == 0 {
		fmt.Fprintf(os.Stderr, "    %s nothing known against this change\n", p.s.wrap(p.s.dim, "·"))
	}
	for _, f := range findings {
		fmt.Fprintf(os.Stderr, "    %s %s\n", p.s.wrap(p.s.red, "!"), f)
	}
	if p.yes("proceed?", true) {
		return Proceed
	}
	return Abort
}

// Failed shows the compiler's own error — not a paraphrase — and offers the two
// outcomes. Neither is chosen automatically: only the operator knows whether the
// tree is worth repairing by hand.
func (p promptDecider) Failed(ctx context.Context, buildOutput string) Decision {
	fmt.Fprintf(os.Stderr, "\n  %s\n", p.s.wrap(p.s.bold+p.s.red, "the composition does not build"))
	for _, line := range strings.Split(strings.TrimRight(buildOutput, "\n"), "\n") {
		fmt.Fprintf(os.Stderr, "    %s\n", p.s.wrap(p.s.dim, line))
	}
	if p.yes("restore the tree as it was?", true) {
		return Restore
	}
	return Keep
}

// yes reads one line and treats anything but an explicit no as the default,
// matching the enter-accepts-the-default idiom of the init wizard.
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
