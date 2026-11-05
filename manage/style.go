package manage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
)

type style struct{ reset, bold, dim, cyan, green, red string }

func newStyle() style {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" || !isTerminal(os.Stderr) {
		return style{}
	}
	return style{
		reset: "\033[0m",
		bold:  "\033[1m",
		dim:   "\033[2m",
		cyan:  "\033[36m",
		green: "\033[32m",
		red:   "\033[31m",
	}
}

func (s style) wrap(code, text string) string {
	if code == "" {
		return text
	}
	return code + text + s.reset
}

func (s style) header(title, sub string) {
	fmt.Fprintf(os.Stderr, "\n  %s\n", s.wrap(s.bold+s.cyan, "herrscher · "+title))
	if sub != "" {
		fmt.Fprintf(os.Stderr, "  %s\n\n", s.wrap(s.dim, sub))
	}
}

func summaryStack(modules []string) {
	sty := newStyle()
	fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.bold, msg(clitext.ManageStackHeader, nil)))
	for _, m := range modules {
		fmt.Fprintf(os.Stderr, "    %s %s\n", sty.wrap(sty.green, "+"), shortModule(m))
	}
	fmt.Fprintln(os.Stderr)
}

func summaryConfig(path string, count int) {
	sty := newStyle()
	fmt.Fprintln(os.Stderr)
	if count > 0 {
		fmt.Fprintf(os.Stderr, "  %s %s\n", sty.wrap(sty.green, "✓"), msg(clitext.ManageWroteSettings, clitext.Params{clitext.ParamCount: count, clitext.ParamPath: path}))
	} else {
		fmt.Fprintf(os.Stderr, "  %s %s\n", sty.wrap(sty.dim, "·"), msg(clitext.ManageNothingEntered, clitext.Params{clitext.ParamPath: path}))
	}
	fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.dim, msg(clitext.ManageInstalledNote, nil)))
	fmt.Fprintf(os.Stderr, "  %s\n\n", msg(clitext.ManageStartWith, clitext.Params{clitext.ParamCommand: sty.wrap(sty.bold, "herrscherd serve")}))
}

func shortModule(m string) string {
	base := filepath.Base(m)
	return strings.TrimPrefix(base, "herrscher-")
}
