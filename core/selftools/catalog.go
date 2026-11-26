package selftools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// ServerName is the name the server is declared under in a session's MCP configuration.
const ServerName = "herrscher"

const (
	commandsTool = "herrscher_commands"
	runTool      = "herrscher_run"
	jsonArg      = "json"
	restArg      = "rest"
	commandArg   = "command"
	argsArg      = "args"
	familyArg    = "family"
	pathJoiner   = "_"
	trueValue    = "true"
)

const serverInstructions = "These tools reach the herrscher daemon that runs this session. " +
	"herrscher_commands lists every verb the daemon knows, with its parameters; herrscher_run runs any of them. " +
	"The daemon decides each call under this session's role: reads pass, and a verb the session may not run " +
	"comes back with the reason and the way to obtain it."

var (
	errUnknownTool    = errors.New("selftools: unknown tool")
	errUnknownCommand = errors.New("selftools: the daemon has no such command")
	errBadArgument    = errors.New("selftools: invalid argument")
	errNoCommand      = errors.New("selftools: command is required")
)

// Daemon runs one argv on the daemon this session belongs to and returns its answer.
type Daemon interface {
	Dispatch(ctx context.Context, argv []string) (string, error)
}

// Exposure is how many verbs become tools of their own.
type Exposure int

const (
	// ExposureCore gives the everyday verbs a tool each and reaches the rest through herrscher_run.
	ExposureCore Exposure = iota
	// ExposureAll gives every verb of the registry a tool of its own.
	ExposureAll
)

var errUnknownExposure = errors.New("selftools: unknown exposure")

// ParseExposure reads an exposure from its name, core when the name is empty.
func ParseExposure(name string) (Exposure, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "core", "on", trueValue, "1":
		return ExposureCore, nil
	case "all":
		return ExposureAll, nil
	}
	return ExposureCore, fmt.Errorf("%w %q", errUnknownExposure, name)
}

// CoreVerbs lists the verbs that get a tool of their own under [ExposureCore]: reads a session runs without asking.
func CoreVerbs() []string {
	return []string{
		"whoami",
		"health",
		"session list",
		"session info",
		"session who",
		"session log",
		"session history",
		"agent list",
		"models list",
		"skill list",
		"schedule list",
		"approve list",
		"memory search",
		"config get",
		"env get",
		"session issue list",
		"session issue show",
	}
}

type param struct {
	Name          string `json:"Name"`
	Help          string `json:"Help"`
	Required      bool   `json:"Required"`
	ValueRequired bool   `json:"ValueRequired"`
}

type spec struct {
	Path   []string `json:"path"`
	Help   string   `json:"help,omitempty"`
	Params []param  `json:"params,omitempty"`
}

func (declared spec) verb() string {
	return strings.Join(declared.Path, " ")
}

func (declared spec) toolName() string {
	return strings.Join(declared.Path, pathJoiner)
}

type catalog struct {
	daemon   Daemon
	exposure Exposure
	mu       sync.Mutex
	specs    []spec
}

func newCatalog(daemon Daemon, exposure Exposure) *catalog {
	return &catalog{daemon: daemon, exposure: exposure}
}

func (c *catalog) refresh(ctx context.Context) ([]spec, error) {
	out, err := c.daemon.Dispatch(ctx, []string{"commands", "--json"})
	if err != nil {
		return nil, err
	}
	var specs []spec
	if err := json.Unmarshal([]byte(out), &specs); err != nil {
		return nil, fmt.Errorf("selftools: read the command list: %w", err)
	}
	c.mu.Lock()
	c.specs = specs
	c.mu.Unlock()
	return specs, nil
}

func (c *catalog) load(ctx context.Context) ([]spec, error) {
	c.mu.Lock()
	cached := c.specs
	c.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	return c.refresh(ctx)
}

func (c *catalog) lookup(ctx context.Context, find func([]spec) (spec, bool)) (spec, bool, error) {
	specs, err := c.load(ctx)
	if err != nil {
		return spec{}, false, err
	}
	if found, ok := find(specs); ok {
		return found, true, nil
	}
	specs, err = c.refresh(ctx)
	if err != nil {
		return spec{}, false, err
	}
	found, ok := find(specs)
	return found, ok, nil
}

func (c *catalog) tools(ctx context.Context) []Tool {
	tools := []Tool{commandsToolDef(), runToolDef()}
	specs, err := c.refresh(ctx)
	if err != nil {
		slog.Warn("self tools: the daemon's verbs are not listed", "err", err)
		return tools
	}
	for _, declared := range specs {
		if c.exposes(declared) {
			tools = append(tools, verbTool(declared))
		}
	}
	return tools
}

func (c *catalog) exposes(declared spec) bool {
	if c.exposure == ExposureAll {
		return true
	}
	return slices.Contains(CoreVerbs(), declared.verb())
}

func (c *catalog) call(ctx context.Context, name string, raw json.RawMessage) (string, error) {
	args, err := decodeArguments(raw)
	if err != nil {
		return "", err
	}
	switch name {
	case commandsTool:
		return c.listCommands(ctx, args)
	case runTool:
		return c.runCommand(ctx, args)
	}
	target, found, err := c.lookup(ctx, func(specs []spec) (spec, bool) {
		for _, declared := range specs {
			if declared.toolName() == name && c.exposes(declared) {
				return declared, true
			}
		}
		return spec{}, false
	})
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("%w %q", errUnknownTool, name)
	}
	return c.dispatch(ctx, target, args)
}

func (c *catalog) listCommands(ctx context.Context, args map[string]any) (string, error) {
	specs, err := c.refresh(ctx)
	if err != nil {
		return "", err
	}
	family, err := optionalString(args, familyArg)
	if err != nil {
		return "", err
	}
	family = strings.TrimSpace(family)
	kept := make([]spec, 0, len(specs))
	for _, declared := range specs {
		if family == "" || (len(declared.Path) > 0 && declared.Path[0] == family) {
			kept = append(kept, declared)
		}
	}
	raw, err := json.Marshal(kept)
	if err != nil {
		return "", fmt.Errorf("selftools: encode the command list: %w", err)
	}
	return string(raw), nil
}

func (c *catalog) runCommand(ctx context.Context, args map[string]any) (string, error) {
	command, err := optionalString(args, commandArg)
	if err != nil {
		return "", err
	}
	path := strings.Fields(command)
	if len(path) == 0 {
		return "", errNoCommand
	}
	target, ok, err := c.lookup(ctx, func(specs []spec) (spec, bool) { return matchSpec(specs, path) })
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%w %q", errUnknownCommand, command)
	}
	flags, err := nestedArguments(args)
	if err != nil {
		return "", err
	}
	flags[restArg] = args[restArg]
	flags[jsonArg] = args[jsonArg]
	return c.dispatch(ctx, target, flags)
}

func (c *catalog) dispatch(ctx context.Context, target spec, args map[string]any) (string, error) {
	argv, err := buildArgv(target, args)
	if err != nil {
		return "", err
	}
	return c.daemon.Dispatch(ctx, argv)
}

func matchSpec(specs []spec, path []string) (spec, bool) {
	for _, declared := range specs {
		if slices.Equal(declared.Path, path) {
			return declared, true
		}
	}
	return spec{}, false
}

func decodeArguments(raw json.RawMessage) (map[string]any, error) {
	args := make(map[string]any)
	if len(raw) == 0 || string(raw) == "null" {
		return args, nil
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("%w: arguments must be an object: %w", errBadArgument, err)
	}
	return args, nil
}

func nestedArguments(args map[string]any) (map[string]any, error) {
	raw, present := args[argsArg]
	if !present || raw == nil {
		return make(map[string]any), nil
	}
	nested, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: %q must be an object", errBadArgument, argsArg)
	}
	return maps.Clone(nested), nil
}

func optionalString(args map[string]any, name string) (string, error) {
	raw, present := args[name]
	if !present || raw == nil {
		return "", nil
	}
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%w: %q must be a string", errBadArgument, name)
	}
	return text, nil
}

func buildArgv(target spec, args map[string]any) ([]string, error) {
	argv := slices.Clone(target.Path)
	for _, declared := range target.Params {
		raw, present := args[declared.Name]
		if !present || raw == nil {
			continue
		}
		tokens, err := flagTokens(declared.Name, raw)
		if err != nil {
			return nil, err
		}
		argv = append(argv, tokens...)
	}
	for name := range args {
		if name == jsonArg || name == restArg || hasParam(target, name) {
			continue
		}
		return nil, fmt.Errorf("%w: %q takes no parameter %q", errBadArgument, target.verb(), name)
	}
	rest, err := restTokens(args[restArg])
	if err != nil {
		return nil, err
	}
	argv = append(argv, rest...)
	asJSON, err := flagValue(jsonArg, args[jsonArg])
	if err != nil {
		return nil, err
	}
	if asJSON == trueValue {
		argv = append(argv, "--json")
	}
	return argv, nil
}

func hasParam(target spec, name string) bool {
	return slices.ContainsFunc(target.Params, func(declared param) bool { return declared.Name == name })
}

func flagTokens(name string, raw any) ([]string, error) {
	value, err := flagValue(name, raw)
	if err != nil {
		return nil, err
	}
	if value == "" {
		return nil, nil
	}
	return []string{"--" + name + "=" + value}, nil
}

func flagValue(name string, raw any) (string, error) {
	switch value := raw.(type) {
	case nil:
		return "", nil
	case string:
		return value, nil
	case bool:
		if value {
			return trueValue, nil
		}
		return "", nil
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), nil
	}
	return "", fmt.Errorf("%w: %q must be a string, a number or a boolean", errBadArgument, name)
}

func restTokens(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: %q must be a list of strings", errBadArgument, restArg)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		text, isText := item.(string)
		if !isText {
			return nil, fmt.Errorf("%w: %q must be a list of strings", errBadArgument, restArg)
		}
		if strings.HasPrefix(text, "-") {
			return nil, fmt.Errorf("%w: %q cannot carry a flag (%q), use the named parameters", errBadArgument, restArg, text)
		}
		out = append(out, text)
	}
	return out, nil
}

// Measure counts the tools a server built on the verbs in specsJSON, the output of `commands --json`, lists under exposure, and the bytes their declarations weigh.
func Measure(specsJSON []byte, exposure Exposure) (int, int, error) {
	var specs []spec
	if err := json.Unmarshal(specsJSON, &specs); err != nil {
		return 0, 0, fmt.Errorf("selftools: read the command list: %w", err)
	}
	listed := &catalog{exposure: exposure, specs: specs}
	tools := []Tool{commandsToolDef(), runToolDef()}
	for _, declared := range specs {
		if listed.exposes(declared) {
			tools = append(tools, verbTool(declared))
		}
	}
	raw, err := json.Marshal(tools)
	if err != nil {
		return 0, 0, fmt.Errorf("selftools: encode the tools: %w", err)
	}
	return len(tools), len(raw), nil
}
