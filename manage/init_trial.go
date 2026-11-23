package manage

import (
	"context"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/internal/errwriter"
)

func noSetting(string) string { return "" }

func trialStack(ctx context.Context, messages *clitext.Messages, out, errOut *errwriter.Writer, modules []string) int {
	chosen, _ := modulesFromPaths(modules)
	specs := backendTrials(chosen, noSetting)
	if len(specs) == 0 {
		return 0
	}
	out.Println(msg(messages, clitext.SetupTrialTitle, nil))
	code := 0
	for _, spec := range specs {
		label := trialLabel(messages, spec)
		output, err := runTrialIn(ctx, execRunner, spec.command, trialTimeout)
		if trialAnswered(string(output), err) {
			out.Println("  ✓ " + label)
			continue
		}
		code = 1
		errOut.Println("  ✗ " + label)
		for _, line := range trialFailureLines(messages, spec, lastLines(string(output), outputTail), err) {
			errOut.Println("    " + line)
		}
	}
	return code
}
