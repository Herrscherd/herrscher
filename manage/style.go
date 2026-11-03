package manage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.bold, "stack"))
	for _, m := range modules {
		fmt.Fprintf(os.Stderr, "    %s %s\n", sty.wrap(sty.green, "+"), shortModule(m))
	}
	fmt.Fprintln(os.Stderr)
}

func summaryConfig(path string, count int) {
	sty := newStyle()
	fmt.Fprintln(os.Stderr)
	if count > 0 {
		fmt.Fprintf(os.Stderr, "  %s wrote %d setting(s) to %s\n", sty.wrap(sty.green, "✓"), count, sty.wrap(sty.bold, path))
	} else {
		fmt.Fprintf(os.Stderr, "  %s nothing entered; %s left unchanged\n", sty.wrap(sty.dim, "·"), path)
	}
	fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.dim, "the installed binary already has its plugins compiled in."))
	fmt.Fprintf(os.Stderr, "  start it with:  %s\n\n", sty.wrap(sty.bold, "herrscherd serve"))
}

func shortModule(m string) string {
	base := filepath.Base(m)
	return strings.TrimPrefix(base, "herrscher-")
}
