package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	orchestrator "github.com/Herrscherd/herrscher-orchestrator"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func addMemoryVerbs(reg *cli.Registry, lang i18n.Language) error {
	return addVerbs(
		reg,
		memoryLocateVerb(),
		memoryForgetVerb(),
		memoryRecordVerb(),
		memoryRestoreVerb(lang),
		memoryUnlinkVerb(lang),
	)
}

func addSkillVerbs(reg *cli.Registry) error {
	return addVerbs(reg, skillListVerb(), skillApproveVerb())
}

func withFirstMemory(ctx context.Context, run func(contracts.Memory) (contracts.Result, error)) (contracts.Result, error) {
	mem, err := BuildFirstMemory(ctx)
	if err != nil {
		return contracts.Result{}, err
	}
	defer closeRead(mem)
	return run(mem)
}

func memoryLocateVerb() contracts.Cmd {
	return contracts.New("memory", "locate").
		Help("print openable URIs (obsidian://, file://) of a memory node's note").
		Param("key", "node key", true).
		Do(func(cmdCtx context.Context, input contracts.Input) (contracts.Result, error) {
			return withFirstMemory(cmdCtx, func(mem contracts.Memory) (contracts.Result, error) {
				return cli.Data(locateMemoryNode(cmdCtx, mem, input))
			})
		})
}

func locateMemoryNode(ctx context.Context, mem contracts.Memory, input contracts.Input) (string, error) {
	loc, ok := mem.(contracts.Locator)
	if !ok {
		return "", errMemoryLocateUnsupported
	}
	line, err := loc.Locate(ctx, input.Get("key"))
	if err != nil {
		return "", err
	}
	if input.JSON {
		b, err := json.Marshal(line)
		return string(b), err
	}
	if line.Obsidian != "" {
		return line.Obsidian, nil
	}
	return line.File, nil
}

func memoryForgetVerb() contracts.Cmd {
	return contracts.New("memory", "forget").
		Help("delete a memory node by key (idempotent)").
		Param("key", "node key", true).
		Do(func(cmdCtx context.Context, input contracts.Input) (contracts.Result, error) {
			return withFirstMemory(cmdCtx, func(mem contracts.Memory) (contracts.Result, error) {
				return cli.Data(forgetMemoryNode(cmdCtx, mem, input))
			})
		})
}

func forgetMemoryNode(ctx context.Context, mem contracts.Memory, input contracts.Input) (string, error) {
	del, ok := mem.(contracts.Deleter)
	if !ok {
		return "", errMemoryForgetUnsupported
	}
	if err := del.Delete(ctx, input.Get("key")); err != nil {
		return "", err
	}
	return "forgotten " + input.Get("key"), nil
}

func memoryRecordVerb() contracts.Cmd {
	return contracts.New("memory", "record").
		Help("upsert a memory node").
		Param("key", "node key", true).
		Param("kind", "node kind (decision, project, …)", true).
		Param("title", "node title", false).
		Param("body", "node body (markdown)", false).
		Do(func(cmdCtx context.Context, input contracts.Input) (contracts.Result, error) {
			return withFirstMemory(cmdCtx, func(mem contracts.Memory) (contracts.Result, error) {
				return cli.Data(recordMemoryNode(cmdCtx, mem, input))
			})
		})
}

func recordMemoryNode(ctx context.Context, mem contracts.Memory, input contracts.Input) (string, error) {
	kind := contracts.NodeKind(input.Get("kind"))
	if !memoryKindAllowed(kind) {
		return "", &UnknownError{Kind: "memory kind", Name: input.Get("kind")}
	}
	node := contracts.Node{
		Key:   input.Get("key"),
		Kind:  kind,
		Title: input.Get("title"),
		Body:  input.Get("body"),
	}
	if err := mem.Record(ctx, node); err != nil {
		return "", err
	}
	return "recorded " + node.Key, nil
}

func memoryRestoreVerb(lang i18n.Language) contracts.Cmd {
	return contracts.New("memory", "restore").
		Help("reactivate an archived (or merged) memory node by key").
		Param("key", "node key", true).
		Param("force", "detach from its umbrella if the node was merged", false).
		Do(func(cmdCtx context.Context, input contracts.Input) (contracts.Result, error) {
			return withFirstMemory(cmdCtx, func(mem contracts.Memory) (contracts.Result, error) {
				if _, err := orchestrator.Restore(cmdCtx, mem, input.Get("key"), orchestrator.Force(input.Bool("force"))); err != nil {
					return contracts.Result{}, err
				}
				return contracts.DataResult(replyText(lang).Text(clitext.HostMemoryRestored, clitext.Params{clitext.ParamKey: input.Get("key")})), nil
			})
		})
}

func memoryUnlinkVerb(lang i18n.Language) contracts.Cmd {
	return contracts.New("memory", "unlink").
		Help("remove the edge from→to between two memory nodes").
		Param("from", "source node key", true).
		Param("to", "target node key", true).
		Do(func(cmdCtx context.Context, input contracts.Input) (contracts.Result, error) {
			return withFirstMemory(cmdCtx, func(mem contracts.Memory) (contracts.Result, error) {
				if err := mem.Unlink(cmdCtx, input.Get("from"), input.Get("to")); err != nil {
					return contracts.Result{}, err
				}
				return contracts.DataResult(replyText(lang).Text(clitext.HostMemoryUnlinked, clitext.Params{clitext.ParamFrom: input.Get("from"), clitext.ParamTo: input.Get("to")})), nil
			})
		})
}

func skillListVerb() contracts.Cmd {
	return contracts.New("skill", "list").
		Help("list the skills agents wrote for themselves, with their state, approval and last use").
		Param("limit", "max rows (default 50)", false).
		Do(func(cmdCtx context.Context, input contracts.Input) (contracts.Result, error) {
			return withFirstMemory(cmdCtx, func(mem contracts.Memory) (contracts.Result, error) {
				return cli.Data(listSkills(cmdCtx, mem, input))
			})
		})
}

const (
	defaultListLimit   = 50
	defaultSearchLimit = 10
)

func listSkills(ctx context.Context, mem contracts.Memory, input contracts.Input) (string, error) {
	hits, err := mem.Search(ctx, contracts.Query{
		Kinds:           []contracts.NodeKind{contracts.KindSkill},
		IncludeArchived: true,
		Limit:           limitOf(input.Get("limit"), defaultListLimit),
	})
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "no learned skills yet", nil
	}
	var body strings.Builder
	for _, n := range hits {
		fmt.Fprintf(&body, "%s\t%s\t%s\tlast used %s\n", n.Key, skillStateOf(n), skillSharingOf(n), lastSeenOf(n))
	}
	return strings.TrimRight(body.String(), "\n"), nil
}

func skillStateOf(n contracts.Node) string {
	if state := n.Meta[contracts.MetaState]; state != "" {
		return state
	}
	return contracts.StateActive
}

func skillSharingOf(n contracts.Node) string {
	if n.Meta[orchestrator.MetaApproved] != "" {
		return "approved"
	}
	return "private"
}

func lastSeenOf(n contracts.Node) string {
	if lastSeen := n.Meta[contracts.MetaLastSeen]; lastSeen != "" {
		return lastSeen
	}
	return "never"
}

func skillApproveVerb() contracts.Cmd {
	return contracts.New("skill", verbApprove).
		Help("let a learned skill be promoted from its agent's private scope to the shared project scope").
		Param("key", "skill node key (see `skill list`)", true).
		Param("revoke", "clear the approval instead; a promotion already made is not undone", false).
		Do(func(cmdCtx context.Context, input contracts.Input) (contracts.Result, error) {
			return withFirstMemory(cmdCtx, func(mem contracts.Memory) (contracts.Result, error) {
				return cli.Data(setSkillApproval(cmdCtx, mem, input))
			})
		})
}

func setSkillApproval(ctx context.Context, mem contracts.Memory, input contracts.Input) (string, error) {
	key := input.Get("key")
	approve := !input.Bool("revoke")
	if err := orchestrator.ApproveSkill(ctx, mem, key, approve); err != nil {
		return "", approvalFailure(err)
	}
	if !approve {
		return "revoked " + key + " (a promotion already made stands; unmake it with `memory unlink` and `memory restore`)", nil
	}
	return "approved " + key, nil
}

func memorySearchVerb() contracts.Cmd {
	return contracts.New("memory", "search").
		Help("full-text search the memory vault; --raw also searches the raw per-turn transcript tier (G7)").
		Param("text", "query text", true).
		Param("raw", "include raw per-turn transcript chunks (G7 archival tier)", false).
		Param("limit", "max hits (default 10)", false).
		Do(func(cmdCtx context.Context, input contracts.Input) (contracts.Result, error) {
			return withFirstMemory(cmdCtx, func(mem contracts.Memory) (contracts.Result, error) {
				return cli.Data(searchMemory(cmdCtx, mem, input))
			})
		})
}

func searchMemory(ctx context.Context, mem contracts.Memory, input contracts.Input) (string, error) {
	hits, err := mem.Search(ctx, contracts.Query{
		Text:       input.Get("text"),
		Ranked:     true,
		Limit:      limitOf(input.Get("limit"), defaultSearchLimit),
		IncludeRaw: input.Bool("raw"),
	})
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "no matches", nil
	}
	var body strings.Builder
	for _, n := range hits {
		fmt.Fprintf(&body, "%s\t[%s]\t%s\n", n.Key, n.Kind, n.Title)
	}
	return strings.TrimRight(body.String(), "\n"), nil
}

func limitOf(raw string, fallback int) int {
	if v, err := strconv.Atoi(raw); err == nil && v > 0 {
		return v
	}
	return fallback
}

func whoamiVerb() contracts.Cmd {
	return contracts.New("whoami").
		Help("print who git on this machine says you are").
		Do(func(ctx context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(WhoamiOut(ctx, in.JSON))
		})
}

func commandsVerb(reg *cli.Registry) contracts.Cmd {
	return contracts.New("commands").
		Help("list every command this daemon dispatches, with its params").
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			if in.JSON {
				b, err := json.Marshal(commandSpecsJSON(reg.Specs()))
				return contracts.DataResult(string(b)), err
			}
			return contracts.DataResult(reg.Help()), nil
		})
}

type paramJSON struct {
	Name          string `json:"Name"`
	Help          string `json:"Help"`
	Required      bool   `json:"Required"`
	ValueRequired bool   `json:"ValueRequired"`
}

type commandSpecJSON struct {
	Path   []string    `json:"path"`
	Help   string      `json:"help,omitempty"`
	Params []paramJSON `json:"params,omitempty"`
}

func commandSpecsJSON(specs []cli.Spec) []commandSpecJSON {
	out := make([]commandSpecJSON, 0, len(specs))
	for _, spec := range specs {
		params := make([]paramJSON, 0, len(spec.Params))
		for _, p := range spec.Params {
			params = append(params, paramJSON(p))
		}
		out = append(out, commandSpecJSON{Path: spec.Path, Help: spec.Help, Params: params})
	}
	return out
}

type unexpectedError struct {
	cause error
}

func (e unexpectedError) Error() string {
	return e.cause.Error()
}

func (e unexpectedError) Unwrap() error {
	return e.cause
}

func (e unexpectedError) Notice() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeUnexpected, Args: []contracts.Arg{contracts.Text(detailParam, e.cause.Error())}}
}

func approvalFailure(err error) error {
	if _, typed := errors.AsType[contracts.NoticeError](err); typed {
		return err
	}
	return unexpectedError{cause: err}
}
