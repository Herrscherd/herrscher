package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/health"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/selftools"
)

// EnvSelfTools switches off the herrscher MCP server a session gets in its worktree when set to a false spelling.
const EnvSelfTools = "HERRSCHER_SELF_TOOLS"

type selfToolsProvision func(ctx context.Context, worktree string) error

func selfToolsPreparer(provision selfToolsProvision, log *slog.Logger) func(context.Context, state.Session) {
	return func(ctx context.Context, sess state.Session) {
		if provision == nil || sess.Worktree == "" {
			return
		}
		if err := provision(ctx, sess.Worktree); err != nil {
			log.Warn("self tools not fully materialized", "session", sess.Name, "err", err)
		}
	}
}

func selfToolsProvisioner(getenv func(string) string, configured config.Config) selfToolsProvision {
	switch strings.ToLower(strings.TrimSpace(getenv(EnvSelfTools))) {
	case "0", "false", "off", "no":
		return nil
	case "":
		if configured.SelfToolsOff() {
			return nil
		}
	}
	return func(ctx context.Context, worktree string) error {
		return agent.MaterializeSelfTools(ctx, worktree, agent.SelfBin())
	}
}

// SelfToolsCallWait is how long a self tool waits for the daemon: long enough for the operator to answer the question a refused verb becomes.
const SelfToolsCallWait = agent.SelfToolsCallWait

// SessionCommandSocketVar names the variable that carries a session's own command socket, the only one the self tools may dial.
const SessionCommandSocketVar = control.CommandSocketVar

func measuredSelfTools(reg *cli.Registry, configured config.Config, getenv func(string) string) func() health.SelfTools {
	return func() health.SelfTools {
		asked := getenv(EnvSelfToolsExpose)
		if strings.TrimSpace(asked) == "" {
			asked = configured.SelfToolsExpose()
		}
		exposure, err := selftools.ParseExposure(asked)
		facts := health.SelfTools{Enabled: selfToolsProvisioner(getenv, configured) != nil, Exposure: exposureName(exposure, err)}
		specs, specsErr := reg.Specs(i18n.English)
		if specsErr != nil {
			slog.Warn("self tools: command help texts not fully rendered", "err", specsErr)
		}
		raw, encodeErr := json.Marshal(commandSpecsJSON(specs))
		if encodeErr != nil {
			return facts
		}
		tools, size, measureErr := selftools.Measure(raw, exposure)
		if measureErr != nil {
			slog.Warn("self tools: not measured", "err", measureErr)
			return facts
		}
		facts.Tools, facts.SchemaBytes = tools, size
		return facts
	}
}

func exposureName(exposure selftools.Exposure, err error) string {
	if err == nil && exposure == selftools.ExposureAll {
		return "all"
	}
	return "core"
}

// EnvSelfToolsExpose picks how many verbs get a tool of their own: core or all.
const EnvSelfToolsExpose = "HERRSCHER_SELF_TOOLS_EXPOSE"

// EnvSelfToolsBytes carries the weight of the self tools declarations down to the bridge, which reports it in each turn's context sizes.
const EnvSelfToolsBytes = "HERRSCHER_SELF_TOOLS_BYTES"

const selfToolsProbeWait = 10 * time.Second

var errSelfToolsSilent = errors.New("herrscher mcp answered, but listed no tool")

func askSelfToolsServer(ctx context.Context, socket string) (int, error) {
	binary := agent.SelfBin()
	if binary == "" {
		return 0, errSelfToolsSilent
	}
	bounded, cancel := context.WithTimeout(ctx, selfToolsProbeWait)
	defer cancel()
	command := exec.CommandContext(bounded, binary, "mcp")
	command.Env = append(os.Environ(), SessionCommandSocketVar+"="+socket)
	command.Stdin = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	out, err := command.Output()
	if err != nil {
		return 0, err
	}
	return listedTools(out)
}

func listedTools(out []byte) (int, error) {
	for _, line := range bytes.Split(out, []byte("\n")) {
		var reply struct {
			ID     int `json:"id"`
			Result struct {
				Tools []json.RawMessage `json:"tools"`
			} `json:"result"`
		}
		if json.Unmarshal(line, &reply) == nil && reply.ID == 2 && len(reply.Result.Tools) > 0 {
			return len(reply.Result.Tools), nil
		}
	}
	return 0, errSelfToolsSilent
}
