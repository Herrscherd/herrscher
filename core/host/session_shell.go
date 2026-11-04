package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	paramCommand       = "cmd"
	shellTimeout       = 2 * time.Minute
	shellOutputCap     = 16 << 10
	shellToolName      = "Bash"
	shellExitFormat    = "\n[sortie %d]"
	shellTruncatedNote = "\n[sortie tronquée]"
)

var (
	errShellRefused        = errors.New("commande refusée")
	errEmptyShellCommand   = errors.New("session shell : la commande est vide")
	errUnknownShellSession = errors.New("session shell : session inconnue")
)

func sessionShellVerb(store *state.State, agents *agent.Store, approvals *approvalTable, sessions *sessionTable) contracts.Cmd {
	return contracts.New(nsSession, "shell").
		Help("lance une commande dans le dossier de la session, après la politique d'approbation ; sa sortie entre dans le prochain message").
		Param(paramName, "nom de la session", true).
		Param(paramCommand, "la ligne de commande, lancée avec sh -c", true).
		Do(func(ctx context.Context, input contracts.Input) (string, error) {
			return runSessionShell(ctx, store, agents, approvals, sessions, input.Get(paramName), input.Get(paramCommand))
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
	pol, mode := sessionPolicy(store, agents, name)
	req := approval.Request{Tool: shellToolName, Subject: command}
	decision, reason := askApproval(ctx, approvals, sessions, name, req, pol, mode, store.ApprovalWait())
	if decision != approval.Allow {
		return "", fmt.Errorf("%w: %s", errShellRefused, reason)
	}
	runCtx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	cmd.Dir = sessionDir(session)
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	text := out.String()
	if len(text) > shellOutputCap {
		text = text[:shellOutputCap] + shellTruncatedNote
	}
	var exit *exec.ExitError
	if errors.As(runErr, &exit) {
		return text + fmt.Sprintf(shellExitFormat, exit.ExitCode()), nil
	}
	return text, runErr
}
