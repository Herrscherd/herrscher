package manage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ANSI styling for the init wizard. Colour is enabled only on a real terminal
// and disabled by $NO_COLOR or TERM=dumb, so piped/scripted output stays clean.
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

// header prints a titled rule, e.g. "  herrscher · init".
func (s style) header(title, sub string) {
	fmt.Fprintf(os.Stderr, "\n  %s\n", s.wrap(s.bold+s.cyan, "herrscher · "+title))
	if sub != "" {
		fmt.Fprintf(os.Stderr, "  %s\n\n", s.wrap(s.dim, sub))
	}
}

// summaryStack prints the resolved module list (compose mode) before the build.
func summaryStack(modules []string) {
	s := newStyle()
	fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.bold, "stack"))
	for _, m := range modules {
		fmt.Fprintf(os.Stderr, "    %s %s\n", s.wrap(s.green, "+"), shortModule(m))
	}
	fmt.Fprintln(os.Stderr)
}

// summaryConfig closes config-only mode: report where secrets landed and how to
// start the already-compiled binary.
func summaryConfig(path string, n int) {
	s := newStyle()
	fmt.Fprintln(os.Stderr)
	if n > 0 {
		fmt.Fprintf(os.Stderr, "  %s wrote %d secret(s) to %s\n", s.wrap(s.green, "✓"), n, s.wrap(s.bold, path))
	} else {
		fmt.Fprintf(os.Stderr, "  %s no secrets entered; %s left unchanged\n", s.wrap(s.dim, "·"), path)
	}
	fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.dim, "the installed binary already has its plugins compiled in."))
	fmt.Fprintf(os.Stderr, "  start it with:  %s\n\n", s.wrap(s.bold, "herrscherd serve"))
}

// shortModule trims the org prefix and the herrscher- prefix from a module path
// for display, e.g. github.com/Herrscherd/herrscher-discord-gateway → discord-gateway.
func shortModule(m string) string {
	base := filepath.Base(m)
	return strings.TrimPrefix(base, "herrscher-")
}
