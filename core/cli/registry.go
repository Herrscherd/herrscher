// Package cli is the native, channel-agnostic command dispatcher. It holds
// declared contracts.Cmd values keyed by their namespace Path and resolves an
// argv invocation to one. It imports only contracts: a command's Run may close
// over anything (a gateway client, a backend), but the registry never sees it.
package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Registry collects commands and dispatches argv to them.
type Registry struct {
	cmds []contracts.Cmd
}

func key(path []string) string { return strings.Join(path, " ") }

// Add registers a command. It rejects an empty path or a duplicate path.
func (r *Registry) Add(c contracts.Cmd) error {
	if len(c.Path) == 0 {
		return fmt.Errorf("cli: command with empty path")
	}
	for _, e := range r.cmds {
		if key(e.Path) == key(c.Path) {
			return fmt.Errorf("cli: duplicate command %q", key(c.Path))
		}
	}
	r.cmds = append(r.cmds, c)
	return nil
}

// Dispatch resolves args to the command whose Path is the longest prefix of
// args, parses the remainder into an Input (--flag value pairs into Args; a
// valueless optional boolean param becomes "true", while an optional
// ValueRequired param still needs a following value; anything left over goes to
// Rest), checks required params, and runs it. It returns the handler's output
// string.
func (r *Registry) Dispatch(ctx context.Context, args []string) (string, error) {
	cmd, rest := r.match(args)
	if cmd == nil {
		return "", fmt.Errorf("unknown command %q", strings.Join(args, " "))
	}
	in, err := parse(*cmd, rest)
	if err != nil {
		return "", err
	}
	return cmd.Run(ctx, in)
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
		return "", fmt.Errorf("unknown command %q", key(path))
	}
	if in.Args == nil {
		in.Args = map[string]string{}
	}
	for _, p := range cmd.Params {
		if p.Required {
			if _, ok := in.Args[p.Name]; !ok {
				return "", fmt.Errorf("%s: missing required --%s", key(cmd.Path), p.Name)
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

// match finds the command whose Path is the longest prefix of args.
func (r *Registry) match(args []string) (*contracts.Cmd, []string) {
	var best *contracts.Cmd
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
		name := strings.TrimPrefix(tok, "--")
		p, ok := isParam(c, name)
		if !ok {
			return in, fmt.Errorf("%s: unknown flag --%s", strings.Join(c.Path, " "), name)
		}
		// A value follows unless the next token is itself a flag. Only an
		// optional boolean param may be valueless.
		if i+1 < len(rest) && !strings.HasPrefix(rest[i+1], "--") {
			in.Args[name] = rest[i+1]
			i++
		} else if p.Required || p.ValueRequired {
			return in, fmt.Errorf("%s: flag --%s needs a value", strings.Join(c.Path, " "), name)
		} else {
			in.Args[name] = "true"
		}
	}
	for _, p := range c.Params {
		if p.Required {
			if _, ok := in.Args[p.Name]; !ok {
				return in, fmt.Errorf("%s: missing required --%s", strings.Join(c.Path, " "), p.Name)
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
		line := "  " + strings.Join(c.Path, " ")
		for _, p := range c.Params {
			if p.Required {
				line += " --" + p.Name + " <" + p.Name + ">"
			} else if p.ValueRequired {
				line += " [--" + p.Name + " <" + p.Name + ">]"
			} else {
				line += " [--" + p.Name + "]"
			}
		}
		if c.Help != "" {
			line += "  — " + c.Help
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
