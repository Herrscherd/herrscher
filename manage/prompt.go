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

// runWizard drives the interactive init: it asks one module kind per category,
// then the gateway's secrets, returning the per-category choices and a secret
// map to write into .env. Every line is read through a single bufio.Reader so
// the masked-secret reader (which toggles terminal echo) shares the buffer and
// loses no input between prompts.
func runWizard() (map[string]string, map[string]string, error) {
	in := bufio.NewReader(os.Stdin)
	fmt.Fprintln(os.Stderr, "herrscher init — compose your plugin stack (enter = default)")
	fmt.Fprintln(os.Stderr)

	choices := map[string]string{}
	for _, cat := range categories {
		kind, err := chooseKind(in, cat)
		if err != nil {
			return nil, nil, err
		}
		choices[cat] = kind
	}

	secrets := map[string]string{}
	if choices["gateway"] == "discord" {
		fmt.Fprintln(os.Stderr, "\ndiscord gateway secrets (enter to skip):")
		tok, err := readSecret(in, "  DISCORD_BOT_TOKEN: ")
		if err != nil {
			return nil, nil, err
		}
		if tok != "" {
			secrets["DISCORD_BOT_TOKEN"] = tok
		}
		if v := promptLine(in, "  DISCORD_CHANNEL_ID (default channel): "); v != "" {
			secrets["DISCORD_CHANNEL_ID"] = v
		}
		if v := promptLine(in, "  DCTL_OWNER_ID: "); v != "" {
			secrets["DCTL_OWNER_ID"] = v
		}
	}
	return choices, secrets, nil
}

// chooseKind prints the catalog for one category (plus a "none" entry) and reads
// a selection: empty = default, a 1-based index, or the kind name itself.
func chooseKind(in *bufio.Reader, cat string) (string, error) {
	kinds := make([]string, 0, len(catalog[cat]))
	for k := range catalog[cat] {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	def := defaultStack[cat]

	fmt.Fprintf(os.Stderr, "%s:\n", cat)
	for i, k := range kinds {
		tag := ""
		if k == def {
			tag = " (default)"
		}
		fmt.Fprintf(os.Stderr, "  %d) %s%s\n", i+1, k, tag)
	}
	fmt.Fprintf(os.Stderr, "  %d) none\n", len(kinds)+1)

	ans := promptLine(in, "> ")
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
