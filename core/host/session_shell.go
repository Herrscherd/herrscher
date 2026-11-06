package host

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher/core/cli"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/internal/boundedbuf"
)

const (
	paramCommand       = "cmd"
	shellTimeout       = 2 * time.Minute
	shellWaitDelay     = 2 * time.Second
	shellOutputCap     = 16 << 10
	shellToolName      = "Bash"
	shellExitFormat    = "\n[sortie %d]"
	shellTruncatedNote = "\n[output truncated]"
)

var (
	errShellRefused        = errors.New("commande refusée")
	errEmptyShellCommand   = errors.New("session shell : la commande est vide")
	errUnknownShellSession = errors.New("session shell : session inconnue")
	errRemoteShellSession  = errors.New("session shell : session distante, commande refusée")
)

func sessionShellVerb(store *state.State, agents *agent.Store, approvals *approvalTable, sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsSession, "shell").
		Help(clitext.CmdHelpSessionShell).
		Param(paramName, clitext.CmdParamSessionShellName, true).
		Param(paramCommand, clitext.CmdParamSessionShellCmd, true).
		Do(func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(runSessionShell(ctx, store, agents, approvals, sessions, input.Get(paramName), input.Get(paramCommand)))
		})
}

func runSessionShell(ctx context.Context, store *state.State, agents *agent.Store, approvals *approvalTable, sessions *sessionTable, name, command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", errEmptyShellCommand
	}
	session, ok := store.FindSession(name)
	if !ok {
		return "", fmt.Errorf("%w %q", errUnknownShellSession, name)
	}
	if session.Host != "" {
		return "", fmt.Errorf("%w %q", errRemoteShellSession, name)
	}
	pol, mode := sessionPolicy(store, agents, name)
	req := approval.Request{Tool: shellToolName, Subject: command}
	decision, reason := askApproval(ctx, approvals, sessions, name, req, pol, mode, store.ApprovalWait())
	if decision != approval.Allow {
		return "", fmt.Errorf("%w: %s", errShellRefused, reason)
	}
	runCtx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()
	out := boundedbuf.New(shellOutputCap)
	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	cmd.Dir = sessionDir(session)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = shellWaitDelay
	runErr := cmd.Run()
	if errors.Is(runErr, exec.ErrWaitDelay) {
		runErr = nil
	}
	text := out.String()
	if out.Truncated() {
		text += shellTruncatedNote
	}
	var exit *exec.ExitError
	if errors.As(runErr, &exit) {
		return text + fmt.Sprintf(shellExitFormat, exit.ExitCode()), nil
	}
	return text, runErr
}
