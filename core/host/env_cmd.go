package host

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
)

const (
	nsEnv          = "env"
	envFileVar     = "HERRSCHER_ENV_FILE"
	envFileMode    = 0o600
	hiddenEnvValue = "(set, hidden)"
	quotedMinimum  = 2
	envQuotable    = " \t#\"'$`\\"
)

var (
	errEnvKey        = errors.New("not a variable name: letters, digits and _, not starting with a digit")
	errEnvValue      = errors.New("a value holds no line break")
	errEnvNoFile     = errors.New("the daemon was started without a settings file, so there is none to edit")
	errEnvNoSuchKey  = errors.New("the settings file does not set this variable")
	errSecretSession = errors.New("a secret is never written by a session: the operator sets it with `herrscher env set` in a terminal")
	envKeyPattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func isSecretEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, marker := range []string{"TOKEN", "KEY", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

type envLine struct {
	raw   string
	key   string
	value string
}

type envDocument struct {
	lines []envLine
}

func readEnvDocument(path string) (envDocument, error) {
	buf, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return envDocument{}, nil
	}
	if err != nil {
		return envDocument{}, err
	}
	var doc envDocument
	for _, raw := range strings.Split(strings.TrimRight(string(buf), "\n"), "\n") {
		doc.lines = append(doc.lines, parsedEnvLine(raw))
	}
	return doc, nil
}

func parsedEnvLine(raw string) envLine {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return envLine{raw: raw}
	}
	if rest, exported := strings.CutPrefix(line, "export"); exported && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
		line = strings.TrimSpace(rest)
	}
	key, value, found := strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	if !found || key == "" {
		return envLine{raw: raw}
	}
	return envLine{raw: raw, key: key, value: unquotedEnvValue(strings.TrimSpace(value))}
}

func unquotedEnvValue(raw string) string {
	if len(raw) >= quotedMinimum {
		if first := raw[0]; (first == '"' || first == '\'') && raw[len(raw)-1] == first {
			return raw[1 : len(raw)-1]
		}
	}
	return raw
}

func (d *envDocument) index(key string) int {
	return slices.IndexFunc(d.lines, func(line envLine) bool { return line.key == key })
}

func (d *envDocument) set(key, value string) {
	rendered := key + "=" + quotedEnvValue(value)
	line := envLine{raw: rendered, key: key, value: value}
	if found := d.index(key); found >= 0 {
		d.lines[found] = line
		return
	}
	d.lines = append(d.lines, line)
}

func (d *envDocument) unset(key string) bool {
	found := d.index(key)
	if found < 0 {
		return false
	}
	d.lines = slices.Delete(d.lines, found, found+1)
	return true
}

func quotedEnvValue(value string) string {
	if !strings.ContainsAny(value, envQuotable) {
		return value
	}
	if !strings.Contains(value, "'") {
		return "'" + value + "'"
	}
	return `"` + value + `"`
}

func (d *envDocument) write(path string) error {
	var out strings.Builder
	for _, line := range d.lines {
		out.WriteString(line.raw)
		out.WriteByte('\n')
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, []byte(out.String()), envFileMode); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func (d *envDocument) render(key string) (string, error) {
	var out []string
	for _, line := range d.lines {
		if line.key == "" || (key != "" && line.key != key) {
			continue
		}
		value := line.value
		if isSecretEnvKey(line.key) && value != "" {
			value = hiddenEnvValue
		}
		out = append(out, line.key+"="+value)
	}
	if key != "" && len(out) == 0 {
		return "", errEnvNoSuchKey
	}
	return strings.Join(out, "\n"), nil
}

func checkEnvKey(key string) error {
	if !envKeyPattern.MatchString(key) {
		return errEnvKey
	}
	return nil
}

func refuseSecretFromSession(ctx context.Context, key string) error {
	if principalOf(ctx).Session != "" && isSecretEnvKey(key) {
		return errSecretSession
	}
	return nil
}

func envVerbs(cliMessages *clitext.Messages, lang languageSource, path func() string, edits *editLock) []contracts.Cmd {
	return []contracts.Cmd{
		contracts.New(nsEnv, "get").
			Help(clitext.CmdHelpEnvGet).
			ValueParam(paramKey, clitext.CmdParamEnvKey, false).
			Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
				file := path()
				if file == "" {
					return contracts.Result{}, errEnvNoFile
				}
				doc, err := readEnvDocument(file)
				if err != nil {
					return contracts.Result{}, err
				}
				return cli.Data(doc.render(strings.TrimSpace(firstOf(input.Get(paramKey), input.Rest))))
			}),
		contracts.New(nsEnv, "set").
			Help(clitext.CmdHelpEnvSet).
			Param(paramKey, clitext.CmdParamEnvKey, true).
			Param(paramValue, clitext.CmdParamEnvValue, true).
			Do(func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
				key, value := strings.TrimSpace(input.Get(paramKey)), input.Get(paramValue)
				if err := refuseSecretFromSession(ctx, key); err != nil {
					return contracts.Result{}, err
				}
				if strings.ContainsAny(value, "\r\n") {
					return contracts.Result{}, errEnvValue
				}
				return cli.Data(editEnv(cliMessages, lang, path(), key, clitext.HostEnvSet, edits, func(doc *envDocument) error {
					doc.set(key, value)
					return nil
				}))
			}),
		contracts.New(nsEnv, "unset").
			Help(clitext.CmdHelpEnvUnset).
			Param(paramKey, clitext.CmdParamEnvKey, true).
			Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
				key := strings.TrimSpace(input.Get(paramKey))
				return cli.Data(editEnv(cliMessages, lang, path(), key, clitext.HostEnvUnset, edits, func(doc *envDocument) error {
					if !doc.unset(key) {
						return errEnvNoSuchKey
					}
					return nil
				}))
			}),
	}
}

func editEnv(cliMessages *clitext.Messages, lang languageSource, path, key string, done clitext.Key, edits *editLock, edit func(*envDocument) error) (string, error) {
	if path == "" {
		return "", errEnvNoFile
	}
	if err := checkEnvKey(key); err != nil {
		return "", err
	}
	release := edits.hold(path)
	defer release()
	doc, err := readEnvDocument(path)
	if err != nil {
		return "", err
	}
	if editErr := edit(&doc); editErr != nil {
		return "", editErr
	}
	if writeErr := doc.write(path); writeErr != nil {
		return "", writeErr
	}
	return replyText(cliMessages, lang()).Text(done, clitext.Params{clitext.ParamKey: key, clitext.ParamPath: path}), nil
}

func daemonEnvFile() string {
	return strings.TrimSpace(os.Getenv(envFileVar))
}
