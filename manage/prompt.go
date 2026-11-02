package manage

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
)

var errChoiceOutOfRange = errors.New("choice out of range")

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func runWizard(compose bool, cat catalogFn, cur routeCurrent) (map[string]string, map[string]string, error) {
	s := newStyle()
	in := bufio.NewReader(os.Stdin)
	choices := map[string]string{}
	for k, v := range defaultStack() {
		choices[k] = v
	}

	if compose {
		s.header("init", "compose your plugin stack: enter accepts the default")
		if err := chooseEveryCategory(s, in, choices); err != nil {
			return nil, nil, err
		}
	} else {
		s.header("init", "configure the installed host: enter to skip a field")
	}

	secrets := map[string]string{}
	if choices[catGateway] == kindDiscord {
		if err := promptDiscordSecrets(s, in, secrets); err != nil {
			return nil, nil, err
		}
	}

	route, err := routeStep(s, in, cat, cur, !compose)
	if err != nil {
		return nil, nil, err
	}
	for k, v := range route {
		secrets[k] = v
	}
	return choices, secrets, nil
}

func chooseEveryCategory(s style, in *bufio.Reader, choices map[string]string) error {
	for _, cat := range categories() {
		kind, err := chooseKind(s, in, cat)
		if err != nil {
			return err
		}
		choices[cat] = kind
	}
	return nil
}

func promptDiscordSecrets(s style, in *bufio.Reader, secrets map[string]string) error {
	fmt.Fprintf(os.Stderr, "  %s  %s\n", s.wrap(s.bold+s.cyan, "discord · secrets"), s.wrap(s.dim, "(token entry is hidden)"))
	tok, err := readSecret(in, secretLabel(s, "DISCORD_BOT_TOKEN"))
	if err != nil {
		return err
	}
	if tok != "" {
		secrets["DISCORD_BOT_TOKEN"] = tok
	}
	if v := promptLine(in, secretLabel(s, "DISCORD_USER_ID")); v != "" {
		secrets["DISCORD_USER_ID"] = v
	}
	if v := promptLine(in, secretLabel(s, "HERRSCHER_OWNER_ID")); v != "" {
		secrets["HERRSCHER_OWNER_ID"] = v
	}
	return nil
}

func secretLabel(s style, key string) string {
	return fmt.Sprintf("    %-21s %s ", key, s.wrap(s.dim, "›"))
}

func chooseKind(s style, in *bufio.Reader, cat string) (string, error) {
	kinds := make([]string, 0, len(catalog()[cat]))
	for k := range catalog()[cat] {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	def := defaultStack()[cat]

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
	switch ans {
	case "":
		return def, nil
	case kindNone:
		return kindNone, nil
	}
	if n, err := strconv.Atoi(ans); err == nil {
		switch {
		case n >= 1 && n <= len(kinds):
			return kinds[n-1], nil
		case n == len(kinds)+1:
			return kindNone, nil
		}
		return "", fmt.Errorf("%s: %w: %d", cat, errChoiceOutOfRange, n)
	}
	if _, ok := catalog()[cat][ans]; ok {
		return ans, nil
	}
	return "", fmt.Errorf("%s: %w %q", cat, errUnknownKind, ans)
}

func promptLine(in *bufio.Reader, msg string) string {
	if msg != "" {
		fmt.Fprint(os.Stderr, msg)
	}
	line, err := in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		slog.Warn("read prompt", "err", err)
	}
	return strings.TrimSpace(line)
}
