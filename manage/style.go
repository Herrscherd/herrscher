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

func (s style) wrap(code, v string) string {
	if code == "" {
		return v
	}
	return code + v + s.reset
}

func (s style) header(title, sub string) {
	_, _ = fmt.Fprintf(os.Stderr, "\n  %s\n", s.wrap(s.bold+s.cyan, "herrscher · "+title))
	if sub != "" {
		_, _ = fmt.Fprintf(os.Stderr, "  %s\n\n", s.wrap(s.dim, sub))
	}
}

func summaryStack(modules []string) {
	s := newStyle()
	_, _ = fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.bold, "stack"))
	for _, m := range modules {
		_, _ = fmt.Fprintf(os.Stderr, "    %s %s\n", s.wrap(s.green, "+"), shortModule(m))
	}
	_, _ = fmt.Fprintln(os.Stderr)
}

func summaryConfig(path string, n int) {
	s := newStyle()
	_, _ = fmt.Fprintln(os.Stderr)
	if n > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "  %s wrote %d setting(s) to %s\n", s.wrap(s.green, "✓"), n, s.wrap(s.bold, path))
	} else {
		_, _ = fmt.Fprintf(os.Stderr, "  %s nothing entered; %s left unchanged\n", s.wrap(s.dim, "·"), path)
	}
	_, _ = fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.dim, "the installed binary already has its plugins compiled in."))
	_, _ = fmt.Fprintf(os.Stderr, "  start it with:  %s\n\n", s.wrap(s.bold, "herrscherd serve"))
}

func shortModule(m string) string {
	base := filepath.Base(m)
	return strings.TrimPrefix(base, "herrscher-")
}
