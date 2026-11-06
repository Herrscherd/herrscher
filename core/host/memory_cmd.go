package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
		memoryForgetVerb(lang),
		memoryRecordVerb(lang),
		memoryRestoreVerb(lang),
		memoryUnlinkVerb(lang),
	)
}

func addSkillVerbs(reg *cli.Registry, lang i18n.Language) error {
	return addVerbs(reg, skillListVerb(lang), skillApproveVerb(lang))
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
		Help(clitext.CmdHelpMemoryLocate).
		Param("key", clitext.CmdParamMemoryLocateKey, true).
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

func memoryForgetVerb(lang i18n.Language) contracts.Cmd {
	return contracts.New("memory", "forget").
		Help(clitext.CmdHelpMemoryForget).
		Param("key", clitext.CmdParamMemoryForgetKey, true).
		Do(func(cmdCtx context.Context, input contracts.Input) (contracts.Result, error) {
			return withFirstMemory(cmdCtx, func(mem contracts.Memory) (contracts.Result, error) {
				return cli.Data(forgetMemoryNode(cmdCtx, lang, mem, input))
			})
		})
}

func forgetMemoryNode(ctx context.Context, lang i18n.Language, mem contracts.Memory, input contracts.Input) (string, error) {
	del, ok := mem.(contracts.Deleter)
	if !ok {
		return "", errMemoryForgetUnsupported
	}
	if err := del.Delete(ctx, input.Get("key")); err != nil {
		return "", err
	}
	return replyText(lang).Text(clitext.HostMemoryForgotten, clitext.Params{clitext.ParamKey: input.Get("key")}), nil
}

func memoryRecordVerb(lang i18n.Language) contracts.Cmd {
	return contracts.New("memory", "record").
		Help(clitext.CmdHelpMemoryRecord).
		Param("key", clitext.CmdParamMemoryRecordKey, true).
		Param("kind", clitext.CmdParamMemoryRecordKind, true).
		Param("title", clitext.CmdParamMemoryRecordTitle, false).
		Param("body", clitext.CmdParamMemoryRecordBody, false).
		Do(func(cmdCtx context.Context, input contracts.Input) (contracts.Result, error) {
			return withFirstMemory(cmdCtx, func(mem contracts.Memory) (contracts.Result, error) {
				return cli.Data(recordMemoryNode(cmdCtx, lang, mem, input))
			})
		})
}

func recordMemoryNode(ctx context.Context, lang i18n.Language, mem contracts.Memory, input contracts.Input) (string, error) {
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
	return replyText(lang).Text(clitext.HostMemoryRecorded, clitext.Params{clitext.ParamKey: node.Key}), nil
}

func memoryRestoreVerb(lang i18n.Language) contracts.Cmd {
	return contracts.New("memory", "restore").
		Help(clitext.CmdHelpMemoryRestore).
		Param("key", clitext.CmdParamMemoryRestoreKey, true).
		Param("force", clitext.CmdParamMemoryRestoreForce, false).
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
		Help(clitext.CmdHelpMemoryUnlink).
		Param("from", clitext.CmdParamMemoryUnlinkFrom, true).
		Param("to", clitext.CmdParamMemoryUnlinkTo, true).
		Do(func(cmdCtx context.Context, input contracts.Input) (contracts.Result, error) {
			return withFirstMemory(cmdCtx, func(mem contracts.Memory) (contracts.Result, error) {
				if err := mem.Unlink(cmdCtx, input.Get("from"), input.Get("to")); err != nil {
					return contracts.Result{}, err
				}
				return contracts.DataResult(replyText(lang).Text(clitext.HostMemoryUnlinked, clitext.Params{clitext.ParamFrom: input.Get("from"), clitext.ParamTo: input.Get("to")})), nil
			})
		})
}

func skillListVerb(lang i18n.Language) contracts.Cmd {
	return contracts.New("skill", "list").
		Help(clitext.CmdHelpSkillList).
		Param("limit", clitext.CmdParamSkillListLimit, false).
		Do(func(cmdCtx context.Context, input contracts.Input) (contracts.Result, error) {
			return withFirstMemory(cmdCtx, func(mem contracts.Memory) (contracts.Result, error) {
				return cli.Data(listSkills(cmdCtx, lang, mem, input))
			})
		})
}

const (
	defaultListLimit   = 50
	defaultSearchLimit = 10
)

func listSkills(ctx context.Context, lang i18n.Language, mem contracts.Memory, input contracts.Input) (string, error) {
	hits, err := mem.Search(ctx, contracts.Query{
		Kinds:           []contracts.NodeKind{contracts.KindSkill},
		IncludeArchived: true,
		Limit:           limitOf(input.Get("limit"), defaultListLimit),
	})
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return replyText(lang).Text(clitext.HostMemorySkillsNone, nil), nil
	}
	var body strings.Builder
	for _, n := range hits {
		fmt.Fprintf(&body, "%s\t%s\t%s\t%s\n", n.Key, skillStateOf(n), skillSharingOf(n), replyText(lang).Text(clitext.HostMemorySkillLastUsed, clitext.Params{clitext.ParamWhen: lastSeenOf(n)}))
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

func skillApproveVerb(lang i18n.Language) contracts.Cmd {
	return contracts.New("skill", verbApprove).
		Help(clitext.CmdHelpSkillApprove).
		Param("key", clitext.CmdParamSkillApproveKey, true).
		Param("revoke", clitext.CmdParamSkillApproveRevoke, false).
		Do(func(cmdCtx context.Context, input contracts.Input) (contracts.Result, error) {
			return withFirstMemory(cmdCtx, func(mem contracts.Memory) (contracts.Result, error) {
				return cli.Data(setSkillApproval(cmdCtx, lang, mem, input))
			})
		})
}

func setSkillApproval(ctx context.Context, lang i18n.Language, mem contracts.Memory, input contracts.Input) (string, error) {
	key := input.Get("key")
	approve := !input.Bool("revoke")
	if err := orchestrator.ApproveSkill(ctx, mem, key, approve); err != nil {
		return "", approvalFailure(err)
	}
	if !approve {
		return replyText(lang).Text(clitext.HostMemorySkillRevoked, clitext.Params{clitext.ParamKey: key}), nil
	}
	return replyText(lang).Text(clitext.HostMemorySkillApproved, clitext.Params{clitext.ParamKey: key}), nil
}

func memorySearchVerb(lang i18n.Language) contracts.Cmd {
	return contracts.New("memory", "search").
		Help(clitext.CmdHelpMemorySearch).
		Param("text", clitext.CmdParamMemorySearchText, true).
		Param("raw", clitext.CmdParamMemorySearchRaw, false).
		Param("limit", clitext.CmdParamMemorySearchLimit, false).
		Do(func(cmdCtx context.Context, input contracts.Input) (contracts.Result, error) {
			return withFirstMemory(cmdCtx, func(mem contracts.Memory) (contracts.Result, error) {
				return cli.Data(searchMemory(cmdCtx, lang, mem, input))
			})
		})
}

func searchMemory(ctx context.Context, lang i18n.Language, mem contracts.Memory, input contracts.Input) (string, error) {
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
		return replyText(lang).Text(clitext.HostMemorySearchNone, nil), nil
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
		Help(clitext.CmdHelpIdentity).
		Do(func(ctx context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(WhoamiOut(ctx, in.JSON))
		})
}

func commandsVerb(lang i18n.Language, reg *cli.Registry) contracts.Cmd {
	return contracts.New("commands").
		Help(clitext.CmdHelpCommands).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			if input.JSON {
				specs, specsErr := reg.Specs(lang)
				if specsErr != nil {
					slog.Warn("command help texts not fully rendered", "err", specsErr)
				}
				raw, marshalErr := json.Marshal(commandSpecsJSON(specs))
				return contracts.DataResult(string(raw)), marshalErr
			}
			help, helpErr := reg.Help(lang)
			if helpErr == nil {
				return contracts.DataResult(help), nil
			}
			warning, renderErr := clitext.For(lang).Render(clitext.CmdHelpCommandsWarning, clitext.Params{clitext.ParamErr: helpErr.Error()})
			if renderErr != nil {
				return contracts.Result{}, renderErr
			}
			return contracts.DataResult(help + "\n" + warning), nil
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
