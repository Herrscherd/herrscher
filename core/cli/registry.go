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
)

// Registry collects commands and dispatches argv to them.
var (
	errEmptyPath        = errors.New("cli: command with empty path")
	errDuplicateCommand = errors.New("cli: duplicate command")
	errUnknownCommand   = errors.New("unknown command")
	errMissingRequired  = errors.New("missing required")
	errUnknownFlag      = errors.New("unknown flag")
	errFlagNeedsValue   = errors.New("needs a value")
)

type Registry struct {
	cmds []contracts.Cmd
}

func key(path []string) string { return strings.Join(path, " ") }

// Add registers a command. It rejects an empty path or a duplicate path.
func (r *Registry) Add(c contracts.Cmd) error {
	if len(c.Path) == 0 {
		return errEmptyPath
	}
	for _, e := range r.cmds {
		if key(e.Path) == key(c.Path) {
			return fmt.Errorf("%w %q", errDuplicateCommand, key(c.Path))
		}
	}
	r.cmds = append(r.cmds, c)
	return nil
}

// Dispatch resolves args to the command whose Path is the longest prefix of
// args, parses the remainder into an Input (--flag value pairs into Args, or
// --flag=value where the value would itself read as a flag; a valueless optional
// boolean param becomes "true", while an optional ValueRequired param still
// needs a following value; anything left over goes to Rest), checks required
// params, and runs it. It returns the handler's output string.
func (r *Registry) Dispatch(ctx context.Context, args []string) (string, error) {
	cmd, rest := r.match(args)
	if cmd == nil {
		return "", fmt.Errorf("%w %q", errUnknownCommand, strings.Join(args, " "))
	}
	in, err := parse(*cmd, rest)
	if err != nil {
		return "", err
	}
	return cmd.Run(ctx, in)
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
	in, err := parse(*cmd, rest)
	if err != nil {
		return cmd.Path, contracts.Input{}, true
	}
	return cmd.Path, in, true
}

// Run invokes the command at an exact path with a pre-built Input, skipping argv
// parsing. It is the typed seam for programmatic callers (e.g. the hub's typed
// SessionControl methods) so they never assemble stringly-typed flag argv that
// could silently drift from the declared params. Required params are still
// checked, so a typed caller gets the same validation as the CLI.
func (r *Registry) Run(ctx context.Context, path []string, in contracts.Input) (string, error) {
	var cmd *contracts.Cmd
	for i := range r.cmds {
		if key(r.cmds[i].Path) == key(path) {
			cmd = &r.cmds[i]
			break
		}
	}
	if cmd == nil {
		return "", fmt.Errorf("%w %q", errUnknownCommand, key(path))
	}
	if in.Args == nil {
		in.Args = map[string]string{}
	}
	for _, p := range cmd.Params {
		if p.Required {
			if _, ok := in.Args[p.Name]; !ok {
				return "", fmt.Errorf("%s: %w --%s", key(cmd.Path), errMissingRequired, p.Name)
			}
		}
	}
	return cmd.Run(ctx, in)
}

// Runner is a command's handler, named so a caller can wrap one.
type Runner func(context.Context, contracts.Input) (string, error)

// Intercept wraps the handler of every registered command whose Path starts with
// prefix. wrap receives the command as declared and its current handler, and
// returns the handler to run in its place; returning next leaves the command
// untouched. Commands added afterwards are not wrapped, so a caller installs its
// interceptors once the registry is complete.
func (r *Registry) Intercept(prefix []string, wrap func(cmd contracts.Cmd, next Runner) Runner) {
	for i := range r.cmds {
		c := &r.cmds[i]
		if len(c.Path) < len(prefix) || !hasPrefix(c.Path, prefix) {
			continue
		}
		c.Run = wrap(*c, c.Run)
	}
}

func (r *Registry) match(args []string) (best *contracts.Cmd, rest []string) {
	bestLen := 0
	for i := range r.cmds {
		c := &r.cmds[i]
		if len(c.Path) > len(args) || len(c.Path) <= bestLen {
			continue
		}
		if hasPrefix(args, c.Path) {
			best = c
			bestLen = len(c.Path)
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
	for _, p := range c.Params {
		if p.Name == name {
			return p, true
		}
	}
	return contracts.Param{}, false
}

func parse(c contracts.Cmd, rest []string) (contracts.Input, error) {
	in := contracts.Input{Args: map[string]string{}}
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if tok == "--json" {
			in.JSON = true
			continue
		}
		if !strings.HasPrefix(tok, "--") {
			in.Rest = append(in.Rest, tok)
			continue
		}
		name, inline, isInline := strings.Cut(strings.TrimPrefix(tok, "--"), "=")
		p, ok := isParam(c, name)
		if !ok {
			return in, fmt.Errorf("%s: %w --%s", strings.Join(c.Path, " "), errUnknownFlag, name)
		}
		if isInline {
			in.Args[name] = inline
			continue
		}
		switch {
		case i+1 < len(rest) && !strings.HasPrefix(rest[i+1], "--"):
			in.Args[name] = rest[i+1]
			i++
		case p.Required || p.ValueRequired:
			return in, fmt.Errorf("%s: flag --%s %w", strings.Join(c.Path, " "), name, errFlagNeedsValue)
		default:
			in.Args[name] = "true"
		}
	}
	for _, p := range c.Params {
		if p.Required {
			if _, ok := in.Args[p.Name]; !ok {
				return in, fmt.Errorf("%s: %w --%s", strings.Join(c.Path, " "), errMissingRequired, p.Name)
			}
		}
	}
	return in, nil
}

// Spec is one command's declared shape, without its Run. Help() renders the same
// facts as a paragraph for a human; a frontend that draws its own menu needs them
// apart, and handing it the contracts.Cmd would hand it the closure too.
type Spec struct {
	Path   []string          `json:"path"`
	Help   string            `json:"help,omitempty"`
	Params []contracts.Param `json:"params,omitempty"`
}

// Specs returns every registered command's shape, sorted by path. It exists so a
// frontend's command menu is derived from what the daemon actually dispatches
// rather than re-typed beside it: a hand-kept list is a list that silently falls
// behind, which is how a palette ends up advertising seven of twenty-nine verbs.
func (r *Registry) Specs() []Spec {
	out := make([]Spec, 0, len(r.cmds))
	for _, c := range r.cmds {
		out = append(out, Spec{Path: c.Path, Help: c.Help, Params: c.Params})
	}
	sort.Slice(out, func(i, j int) bool { return key(out[i].Path) < key(out[j].Path) })
	return out
}

// Help renders one usage line per command, sorted by path, for the root help.
func (r *Registry) Help() string {
	lines := make([]string, 0, len(r.cmds))
	for _, c := range r.cmds {
		var line strings.Builder
		line.WriteString("  " + strings.Join(c.Path, " "))
		for _, p := range c.Params {
			switch {
			case p.Required:
				line.WriteString(" --" + p.Name + " <" + p.Name + ">")
			case p.ValueRequired:
				line.WriteString(" [--" + p.Name + " <" + p.Name + ">]")
			default:
				line.WriteString(" [--" + p.Name + "]")
			}
		}
		if c.Help != "" {
			line.WriteString(": " + c.Help)
		}
		lines = append(lines, line.String())
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
