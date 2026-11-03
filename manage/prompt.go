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
	sty := newStyle()
	input := bufio.NewReader(os.Stdin)
	choices := make(map[string]string)
	for k, v := range defaultStack() {
		choices[k] = v
	}

	if compose {
		sty.header("init", "compose your plugin stack: enter accepts the default")
		if err := chooseEveryCategory(sty, input, choices); err != nil {
			return nil, nil, err
		}
	} else {
		sty.header("init", "configure the installed host: enter to skip a field")
	}

	secrets := make(map[string]string)
	if choices[catGateway] == kindDiscord {
		if err := promptDiscordSecrets(sty, input, secrets); err != nil {
			return nil, nil, err
		}
	}

	route, err := routeStep(sty, input, cat, cur, !compose)
	if err != nil {
		return nil, nil, err
	}
	for k, v := range route {
		secrets[k] = v
	}
	return choices, secrets, nil
}

func chooseEveryCategory(sty style, input *bufio.Reader, choices map[string]string) error {
	for _, cat := range categories() {
		kind, err := chooseKind(sty, input, cat)
		if err != nil {
			return err
		}
		choices[cat] = kind
	}
	return nil
}

func promptDiscordSecrets(sty style, input *bufio.Reader, secrets map[string]string) error {
	_, _ = fmt.Fprintf(os.Stderr, "  %s  %s\n", sty.wrap(sty.bold+sty.cyan, "discord · secrets"), sty.wrap(sty.dim, "(token entry is hidden)"))
	tok, err := readSecret(input, secretLabel(sty, "DISCORD_BOT_TOKEN"))
	if err != nil {
		return err
	}
	if tok != "" {
		secrets["DISCORD_BOT_TOKEN"] = tok
	}
	if v := promptLine(input, secretLabel(sty, "DISCORD_USER_ID")); v != "" {
		secrets["DISCORD_USER_ID"] = v
	}
	if v := promptLine(input, secretLabel(sty, "HERRSCHER_OWNER_ID")); v != "" {
		secrets["HERRSCHER_OWNER_ID"] = v
	}
	return nil
}

func secretLabel(s style, key string) string {
	return fmt.Sprintf("    %-21s %s ", key, s.wrap(s.dim, "›"))
}

func chooseKind(sty style, input *bufio.Reader, cat string) (string, error) {
	kinds := make([]string, 0, len(catalog()[cat]))
	for k := range catalog()[cat] {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	def := defaultStack()[cat]

	_, _ = fmt.Fprintf(os.Stderr, "  %s\n", sty.wrap(sty.bold, cat))
	for idx, kind := range kinds {
		name := kind
		tag := ""
		if kind == def {
			name = sty.wrap(sty.green, kind)
			tag = sty.wrap(sty.dim, "  default")
		}
		_, _ = fmt.Fprintf(os.Stderr, "    %s %s%s\n", sty.wrap(sty.dim, strconv.Itoa(idx+1)), name, tag)
	}
	_, _ = fmt.Fprintf(os.Stderr, "    %s %s\n", sty.wrap(sty.dim, strconv.Itoa(len(kinds)+1)), "none")

	ans := promptLine(input, "  "+sty.wrap(sty.dim, "›")+" ")
	switch ans {
	case "":
		return def, nil
	case kindNone:
		return kindNone, nil
	}
	if choice, err := strconv.Atoi(ans); err == nil {
		switch {
		case choice >= 1 && choice <= len(kinds):
			return kinds[choice-1], nil
		case choice == len(kinds)+1:
			return kindNone, nil
		}
		return "", fmt.Errorf("%s: %w: %d", cat, errChoiceOutOfRange, choice)
	}
	if _, ok := catalog()[cat][ans]; ok {
		return ans, nil
	}
	return "", fmt.Errorf("%s: %w %q", cat, errUnknownKind, ans)
}

func promptLine(input *bufio.Reader, msg string) string {
	if msg != "" {
		_, _ = fmt.Fprint(os.Stderr, msg)
	}
	line, err := input.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		slog.Warn("read prompt", "err", err)
	}
	return strings.TrimSpace(line)
}
