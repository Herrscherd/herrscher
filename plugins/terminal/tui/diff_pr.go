package tui

import (
	"context"
	"errors"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/forgeport"
	"github.com/Herrscherd/herrscher/core/health"
)

const (
	prOpenLimit   = 2 * time.Minute
	commitPRLimit = gitCallLimit + prCallLimit + prOpenLimit
)

type dispatchReply struct {
	out string
	err error
}

func dispatchWithin(ctx context.Context, backend Backend, args []string) (string, error) {
	replies := make(chan dispatchReply, 1)
	go func() {
		out, err := backend.Dispatch(args)
		replies <- dispatchReply{out: out, err: err}
	}()
	select {
	case reply := <-replies:
		return reply.out, reply.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func requireGitHub(ctx context.Context, backend Backend) error {
	out, err := dispatchWithin(ctx, backend, []string{verbHealth})
	if err != nil {
		return err
	}
	snapshot, err := health.Decode([]byte(out))
	if err != nil {
		return err
	}
	checks := snapshot.Find(forgeport.KindGitHub)
	for index := range checks {
		check := &checks[index]
		if check.Group == health.GroupForge && check.Report.State == contracts.HealthOK {
			return nil
		}
	}
	return contracts.ErrForgeNotConnected
}

func openPullRequest(parent context.Context, backend Backend, trans translator, sha, session, subject, body, base string, limit time.Duration) diffActionMsg {
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	out, err := dispatchWithin(ctx, backend, sessionPROpenArgs(session, subject, body, base))
	if errors.Is(err, context.DeadlineExceeded) {
		return diffActionMsg{notice: trans.t("tui.diffpanel.pr_timed_out", map[string]any{tplArgSha: sha}), bad: true}
	}
	if err != nil {
		return diffActionMsg{notice: trans.t("tui.diffpanel.pr_failed", map[string]any{tplArgSha: sha, tplArgReason: localizedReason(trans, err)}), bad: true}
	}
	return diffActionMsg{notice: trans.t("tui.diffpanel.pr_opened", map[string]any{tplArgSha: sha, tplArgResult: out})}
}

func sessionPROpenArgs(session, title, body, base string) []string {
	return []string{sessionVerb, "pr", "open", "--name", session, "--title", title, "--body", body, "--base", base}
}
