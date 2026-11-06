// Package cli is the native, channel-agnostic command dispatcher. It holds
// declared contracts.Cmd values keyed by their namespace Path and resolves an
// argv invocation to one. It imports only contracts: a command's Run may close
// over anything (a gateway client, a backend), but the registry never sees it.
package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

var (
	errEmptyPath        = errors.New("cli: command with empty path")
	errDuplicateCommand = errors.New("cli: duplicate command")
	errUnknownCommand   = errors.New("unknown command")
	errMissingRequired  = errors.New("missing required")
	errUnknownFlag      = errors.New("unknown flag")
	errFlagNeedsValue   = errors.New("needs a value")
)

// Registry holds the commands a process can run, keyed by their verb path.
type Registry struct {
	cmds []contracts.Cmd
}

func key(path []string) string { return strings.Join(path, " ") }

// Add registers a command. It rejects an empty path or a duplicate path.
func (r *Registry) Add(cmd contracts.Cmd) error {
	if len(cmd.Path) == 0 {
		return errEmptyPath
	}
	for _, e := range r.cmds {
		if key(e.Path) == key(cmd.Path) {
			return fmt.Errorf("%w %q", errDuplicateCommand, key(cmd.Path))
		}
	}
	r.cmds = append(r.cmds, cmd)
	return nil
}

// Dispatch runs the command named by args and returns its result unrendered.
func (r *Registry) Dispatch(ctx context.Context, args []string) (contracts.Result, error) {
	cmd, rest := r.match(args)
	if cmd == nil {
		return contracts.Result{}, fmt.Errorf("%w %q", errUnknownCommand, strings.Join(args, " "))
	}
	input, err := parse(*cmd, rest)
	if err != nil {
		return contracts.Result{}, err
	}
	return cmd.Run(ctx, input)
}

// Text renders result in lang: its notice when it has one, otherwise its data.
func Text(result contracts.Result, lang i18n.Language) (string, error) {
	lang = orEnglish(lang)
	notice, ok := result.Notice()
	if ok {
		return contracts.NewRenderer().Render(lang, notice)
	}
	data := result.Data()
	if data == nil {
		return "", nil
	}
	if text, isText := data.(string); isText {
		return text, nil
	}
	return fmt.Sprint(data), nil
}

// Render returns the text of a command outcome in lang: the error rendered in lang when it failed,
// otherwise the result rendered by Text.
func Render(res contracts.Result, err error, lang i18n.Language) (string, error) {
	if err != nil {
		return "", Localize(err, lang)
	}
	return Text(res, lang)
}

// Resolve reports the command args address and the Input parsed for it, without
// running anything. It exists so a caller that must decide something ABOUT an
// invocation, rather than perform it, reads the same command path and the same
// parsed flags Dispatch would. The alternative is re-reading argv beside
// Dispatch, which is how a check ends up disagreeing with what actually runs.
//
// A parse failure still yields the path and ok: what the caller asked for is
// already known, and the error itself is Dispatch's to report a moment later.
// The Input is empty there, so a decision that reads a flag sees no flag rather
// than a stale one.
func (r *Registry) Resolve(args []string) ([]string, contracts.Input, bool) {
	cmd, rest := r.match(args)
	if cmd == nil {
		return nil, contracts.Input{}, false
	}
	input, err := parse(*cmd, rest)
	if err != nil {
		return cmd.Path, contracts.Input{}, true
	}
	return cmd.Path, input, true
}

// Run invokes the command at an exact path with a pre-built Input, skipping argv
// parsing. It is the typed seam for programmatic callers (e.g. the hub's typed
// SessionControl methods) so they never assemble stringly-typed flag argv that
// could silently drift from the declared params. Required params are still
// checked, so a typed caller gets the same validation as the CLI.
func (r *Registry) Run(ctx context.Context, path []string, input contracts.Input) (contracts.Result, error) {
	var cmd *contracts.Cmd
	for index := range r.cmds {
		if key(r.cmds[index].Path) == key(path) {
			cmd = &r.cmds[index]
			break
		}
	}
	if cmd == nil {
		return contracts.Result{}, fmt.Errorf("%w %q", errUnknownCommand, key(path))
	}
	if input.Args == nil {
		input.Args = make(map[string]string)
	}
	for _, param := range cmd.Params {
		if param.Required {
			if _, ok := input.Args[param.Name]; !ok {
				return contracts.Result{}, fmt.Errorf("%s: %w --%s", key(cmd.Path), errMissingRequired, param.Name)
			}
		}
	}
	return cmd.Run(ctx, input)
}

// Runner is a command's handler, named so a caller can wrap one.
type Runner func(context.Context, contracts.Input) (contracts.Result, error)

// Intercept wraps the handler of every registered command whose Path starts with
// prefix. wrap receives the command as declared and its current handler, and
// returns the handler to run in its place; returning next leaves the command
// untouched. Commands added afterwards are not wrapped, so a caller installs its
// interceptors once the registry is complete.
func (r *Registry) Intercept(prefix []string, wrap func(cmd contracts.Cmd, next Runner) Runner) {
	for i := range r.cmds {
		cmd := &r.cmds[i]
		if len(cmd.Path) < len(prefix) || !hasPrefix(cmd.Path, prefix) {
			continue
		}
		cmd.Run = wrap(*cmd, cmd.Run)
	}
}

func (r *Registry) match(args []string) (*contracts.Cmd, []string) {
	var best *contracts.Cmd
	bestLen := 0
	for i := range r.cmds {
		cmd := &r.cmds[i]
		if len(cmd.Path) > len(args) || len(cmd.Path) <= bestLen {
			continue
		}
		if hasPrefix(args, cmd.Path) {
			best = cmd
			bestLen = len(cmd.Path)
		}
	}
	if best == nil {
		return nil, nil
	}
	return best, args[bestLen:]
}

func hasPrefix(args, path []string) bool {
	for i, p := range path {
		if args[i] != p {
			return false
		}
	}
	return true
}

// FlagArg spells a flag and its value as the tokens a dispatch should carry.
//
// parse takes the token after a flag as that flag's value only when it does not
// itself begin with "--", so a value that does needs the inline "--name=value"
// form. Free text is where this happens: a task, a typed message, anything an
// operator or an agent wrote rather than chose.
//
// The inline form is used only where it is needed, and that restraint is the
// point. The two-token form is what every released daemon parses, and an
// attached CLI dials a daemon it may predate; switching every argv to a shape an
// older parser reads as one unknown flag would break every message to buy back
// the one that was already broken.
func FlagArg(name, value string) []string {
	if strings.HasPrefix(value, "--") {
		return []string{"--" + name + "=" + value}
	}
	return []string{"--" + name, value}
}

func isParam(c contracts.Cmd, name string) (contracts.Param, bool) {
	for _, param := range c.Params {
		if param.Name == name {
			return param, true
		}
	}
	return contracts.Param{}, false
}

func parse(cmd contracts.Cmd, rest []string) (contracts.Input, error) {
	input := contracts.Input{Args: make(map[string]string)}
	for index := 0; index < len(rest); index++ {
		tok := rest[index]
		switch {
		case tok == "--json":
			input.JSON = true
		case !strings.HasPrefix(tok, "--"):
			input.Rest = append(input.Rest, tok)
		default:
			consumed, err := parseFlag(cmd, rest, index, input)
			if err != nil {
				return input, err
			}
			index += consumed
		}
	}
	return input, checkRequired(cmd, input)
}

func parseFlag(cmd contracts.Cmd, rest []string, index int, input contracts.Input) (int, error) {
	name, inline, isInline := strings.Cut(strings.TrimPrefix(rest[index], "--"), "=")
	param, ok := isParam(cmd, name)
	if !ok {
		return 0, fmt.Errorf("%s: %w --%s", strings.Join(cmd.Path, " "), errUnknownFlag, name)
	}
	if isInline {
		input.Args[name] = inline
		return 0, nil
	}
	switch {
	case index+1 < len(rest) && !strings.HasPrefix(rest[index+1], "--"):
		input.Args[name] = rest[index+1]
		return 1, nil
	case param.Required || param.ValueRequired:
		return 0, fmt.Errorf("%s: flag --%s %w", strings.Join(cmd.Path, " "), name, errFlagNeedsValue)
	default:
		input.Args[name] = "true"
		return 0, nil
	}
}

func checkRequired(cmd contracts.Cmd, input contracts.Input) error {
	for _, param := range cmd.Params {
		if !param.Required {
			continue
		}
		if _, ok := input.Args[param.Name]; !ok {
			return fmt.Errorf("%s: %w --%s", strings.Join(cmd.Path, " "), errMissingRequired, param.Name)
		}
	}
	return nil
}

// Spec is one command's declared shape, without its Run. Help() renders the same
// facts as a paragraph for a human; a frontend that draws its own menu needs them
// apart, and handing it the contracts.Cmd would hand it the closure too.
type Spec struct {
	Path   []string          `json:"path"`
	Help   string            `json:"help,omitempty"`
	Params []contracts.Param `json:"params,omitempty"`
}

// Specs returns every registered command's shape, sorted by path, with its help
// texts rendered in lang. It exists so a frontend's command menu is derived from
// what the daemon actually dispatches rather than re-typed beside it: a hand-kept
// list is a list that silently falls behind, which is how a palette ends up
// advertising seven of twenty-nine verbs.
func (r *Registry) Specs(lang i18n.Language) []Spec {
	translator := clitext.For(lang)
	out := make([]Spec, 0, len(r.cmds))
	for _, c := range r.cmds {
		out = append(out, Spec{Path: c.Path, Help: helpText(translator, c.Help), Params: localizedParams(translator, c.Params)})
	}
	sort.Slice(out, func(i, j int) bool { return key(out[i].Path) < key(out[j].Path) })
	return out
}

func helpText(translator *clitext.Translator, helpKey string) string {
	if helpKey == "" {
		return ""
	}
	return translator.Text(clitext.Key(helpKey), nil)
}

func localizedParams(translator *clitext.Translator, params []contracts.Param) []contracts.Param {
	if len(params) == 0 {
		return nil
	}
	out := make([]contracts.Param, 0, len(params))
	for _, param := range params {
		param.Help = helpText(translator, param.Help)
		out = append(out, param)
	}
	return out
}

// Help renders one usage line per command, sorted by path, for the root help, with
// its help text rendered in lang.
func (r *Registry) Help(lang i18n.Language) string {
	translator := clitext.For(lang)
	lines := make([]string, 0, len(r.cmds))
	for _, cmd := range r.cmds {
		var line strings.Builder
		line.WriteString("  " + strings.Join(cmd.Path, " "))
		for _, param := range cmd.Params {
			switch {
			case param.Required:
				line.WriteString(" --" + param.Name + " <" + param.Name + ">")
			case param.ValueRequired:
				line.WriteString(" [--" + param.Name + " <" + param.Name + ">]")
			default:
				line.WriteString(" [--" + param.Name + "]")
			}
		}
		if help := helpText(translator, cmd.Help); help != "" {
			line.WriteString(": " + help)
		}
		lines = append(lines, line.String())
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// Data wraps the output of a text handler as a Result.
func Data(text string, err error) (contracts.Result, error) {
	if err != nil {
		return contracts.Result{}, err
	}
	return contracts.DataResult(text), nil
}

func orEnglish(lang i18n.Language) i18n.Language {
	if lang == 0 {
		return i18n.English
	}
	return lang
}

type renderedError struct {
	text string
	err  error
}

func (e renderedError) Error() string { return e.text }

func (e renderedError) Unwrap() error { return e.err }

// Localize returns err with its notice rendered in lang, or err itself when it carries none.
func Localize(err error, lang i18n.Language) error {
	var noticed contracts.NoticeError
	if err == nil || !errors.As(err, &noticed) {
		return err
	}
	text, renderErr := renderNoticeText(noticed.Notice(), orEnglish(lang))
	if renderErr != nil {
		return err
	}
	return renderedError{text: text, err: err}
}

func renderNoticeText(notice contracts.Notice, lang i18n.Language) (string, error) {
	text, err := contracts.NewRenderer().Render(lang, notice)
	if err != nil || notice.Code != contracts.CodeSessionCloseDirty {
		return text, err
	}
	kept, err := clitext.Text(lang, clitext.SessionCloseBranchKept, clitext.Params{clitext.ParamBranch: "session/" + noticeArg(notice, clitext.ParamName)})
	if err != nil {
		return "", err
	}
	return text + "\n" + kept, nil
}

func noticeArg(notice contracts.Notice, name string) string {
	for _, arg := range notice.Args {
		if value, ok := arg.Value().(string); ok && arg.Name() == name {
			return value
		}
	}
	return ""
}

// Handler adapts a text handler to the Result form a command runs.
func Handler(handle func(context.Context, contracts.Input) (string, error)) func(context.Context, contracts.Input) (contracts.Result, error) {
	return func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
		return Data(handle(ctx, input))
	}
}

// Plain renders res or err as the English text a text-only caller expects.
func Plain(res contracts.Result, err error) (string, error) {
	if err != nil {
		return "", Localize(err, i18n.English)
	}
	return Text(res, i18n.English)
}
