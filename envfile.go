package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// loadEnvFile reads KEY=VALUE lines from path into the process environment so
// the host's secrets (DISCORD_BOT_TOKEN, DCTL_OWNER_ID, …) need not be sourced
// by a shell wrapper — herrscher auto-loads ./.env at startup (overridable with
// $HERRSCHER_ENV_FILE) and the service passes its file via `herrscher serve
// --env-file PATH`. Blank lines and `#` comments are skipped, an optional
// leading `export ` (any whitespace) is ignored, surrounding quotes on the value
// are stripped, and only the first `=` splits key from value (values may contain
// `=`). Keys already present in the environment are left untouched (real env
// wins). A missing file is not an error — the service may be installed before
// its secrets are filled in.
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open env file: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// Raise the per-line cap (default 64KB) so long single-line secrets — PEM
	// keys, JWTs — don't trip bufio.ErrTooLong and abort the load.
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip a leading `export` keyword followed by any whitespace (space or
		// tab), not just a single space.
		if rest, ok := strings.CutPrefix(line, "export"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			line = strings.TrimSpace(rest)
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue // not a KEY=VALUE line
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, set := os.LookupEnv(key); set {
			continue // real environment takes precedence over the file
		}
		if err := os.Setenv(key, unquoteEnv(strings.TrimSpace(val))); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}
	return sc.Err()
}

// unquoteEnv strips a single matching pair of surrounding single or double
// quotes, so `KEY="a b"` and `KEY='a b'` yield `a b`.
func unquoteEnv(s string) string {
	if len(s) >= 2 {
		if c := s[0]; (c == '"' || c == '\'') && s[len(s)-1] == c {
			return s[1 : len(s)-1]
		}
	}
	return s
}
