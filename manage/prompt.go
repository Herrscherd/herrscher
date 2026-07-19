package manage

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// isTerminal reports whether f is an interactive terminal (a char device),
// gating the init wizard: we only prompt when there's a human to answer.
func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// runWizard drives the interactive init. In compose mode it asks one module kind
// per category; in config-only mode the plugin set is fixed (already compiled
// in), so it skips straight to the gateway secrets. Either way it returns the
// per-category choices and a secret map to persist. Every line is read through a
// single bufio.Reader so the masked-secret reader (which toggles terminal echo)
// shares the buffer and loses no input between prompts.
func runWizard(compose bool) (map[string]string, map[string]string, error) {
	s := newStyle()
	in := bufio.NewReader(os.Stdin)
	choices := map[string]string{}
	for k, v := range defaultStack {
		choices[k] = v
	}

	if compose {
		s.header("init", "compose your plugin stack — enter accepts the default")
		for _, cat := range categories {
			kind, err := chooseKind(s, in, cat)
			if err != nil {
				return nil, nil, err
			}
			choices[cat] = kind
		}
	} else {
		s.header("init", "configure the installed host — enter to skip a field")
	}

	secrets := map[string]string{}
	if choices["gateway"] == "discord" {
		fmt.Fprintf(os.Stderr, "  %s  %s\n", s.wrap(s.bold+s.cyan, "discord · secrets"), s.wrap(s.dim, "(token entry is hidden)"))
		tok, err := readSecret(in, secretLabel(s, "DISCORD_BOT_TOKEN"))
		if err != nil {
			return nil, nil, err
		}
		if tok != "" {
			secrets["DISCORD_BOT_TOKEN"] = tok
		}
		if v := promptLine(in, secretLabel(s, "DISCORD_CHANNEL_ID")); v != "" {
			secrets["DISCORD_CHANNEL_ID"] = v
		}
		if v := promptLine(in, secretLabel(s, "HERRSCHER_OWNER_ID")); v != "" {
			secrets["HERRSCHER_OWNER_ID"] = v
		}
	}
	return choices, secrets, nil
}

// secretLabel renders an aligned, dimmed key followed by the prompt arrow.
func secretLabel(s style, key string) string {
	return fmt.Sprintf("    %-20s %s ", key, s.wrap(s.dim, "›"))
}

// chooseKind prints the catalog for one category (plus a "none" entry) and reads
// a selection: empty = default, a 1-based index, or the kind name itself.
func chooseKind(s style, in *bufio.Reader, cat string) (string, error) {
	kinds := make([]string, 0, len(catalog[cat]))
	for k := range catalog[cat] {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	def := defaultStack[cat]

	fmt.Fprintf(os.Stderr, "  %s\n", s.wrap(s.bold, cat))
	for i, k := range kinds {
		name := k
		tag := ""
		if k == def {
			name = s.wrap(s.green, k)
			tag = s.wrap(s.dim, "  default")
		}
		fmt.Fprintf(os.Stderr, "    %s %s%s\n", s.wrap(s.dim, strconv.Itoa(i+1)), name, tag)
	}
	fmt.Fprintf(os.Stderr, "    %s %s\n", s.wrap(s.dim, strconv.Itoa(len(kinds)+1)), "none")

	ans := promptLine(in, "  "+s.wrap(s.dim, "›")+" ")
	switch {
	case ans == "":
		return def, nil
	case ans == "none":
		return "none", nil
	}
	if n, err := strconv.Atoi(ans); err == nil {
		switch {
		case n >= 1 && n <= len(kinds):
			return kinds[n-1], nil
		case n == len(kinds)+1:
			return "none", nil
		}
		return "", fmt.Errorf("%s: choice out of range: %d", cat, n)
	}
	if _, ok := catalog[cat][ans]; ok {
		return ans, nil
	}
	return "", fmt.Errorf("unknown %s kind %q", cat, ans)
}

// promptLine writes msg to stderr and returns the next trimmed input line.
func promptLine(in *bufio.Reader, msg string) string {
	if msg != "" {
		fmt.Fprint(os.Stderr, msg)
	}
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}
