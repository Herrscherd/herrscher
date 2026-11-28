package host

import (
	"context"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/forge"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	verbEnd   = "end"
	verbClose = "close"
)

var (
	errSessionEndNoPR     = sessionPRError{clitext.SessionEndNoPR}
	errSessionEndOpen     = sessionPRError{clitext.SessionEndOpen}
	errSessionEndDetached = sessionPRError{clitext.SessionEndDetached}
	errSessionEndDirty    = sessionPRError{clitext.SessionEndDirty}
	errSessionEndUnpushed = sessionPRError{clitext.SessionEndUnpushed}
)

type sessionEndTarget struct {
	language i18n.Language
	record   state.PullRequestRecord
	branch   string
	common   string
	env      []string
	squashed bool
}

func (p sessionPRs) endVerb(reg *cli.Registry) contracts.Cmd {
	closeSession := func(ctx context.Context, name string) error {
		_, err := reg.Run(ctx, []string{nsSession, verbClose}, contracts.Input{Args: map[string]string{paramName: name}})
		return err
	}
	return contracts.New(nsSession, verbEnd).
		Help(clitext.CmdHelpSessionEnd).
		Param(paramName, clitext.CmdParamSessionEndName, true).
		Do(func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
			return p.end(ctx, input.Get(paramName), closeSession)
		})
}

func (p sessionPRs) end(ctx context.Context, name string, closeSession func(context.Context, string) error) (contracts.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, p.forgeLimit)
	defer cancel()
	target, err := p.endTarget(ctx, name)
	if err != nil {
		return contracts.Result{}, err
	}
	if err := closeSession(ctx, name); err != nil {
		return contracts.Result{}, err
	}
	return p.endOutcome(ctx, name, target)
}

func (p sessionPRs) endTarget(ctx context.Context, name string) (sessionEndTarget, error) {
	sess, err := p.session(name)
	if err != nil {
		return sessionEndTarget{}, err
	}
	language := namedSessionLanguage(p.store, name, p.lang())
	if refusal := p.remoteRefusal(language, name, sess); refusal != nil {
		return sessionEndTarget{}, refusal
	}
	record := sess.PullRequestRecord()
	if record.Number == 0 {
		return sessionEndTarget{}, p.messages.Failure(language, errSessionEndNoPR, errSessionEndNoPR.key, clitext.Params{clitext.ParamName: escapeLine(name)})
	}
	if !record.Ended() {
		return sessionEndTarget{}, p.messages.Failure(language, errSessionEndOpen, errSessionEndOpen.key, clitext.Params{clitext.ParamName: escapeLine(name), clitext.ParamNumber: record.Number})
	}
	dir, err := p.workDir(language, name, sess)
	if err != nil {
		return sessionEndTarget{}, err
	}
	branch, err := prGit(ctx, dir, nil, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch == "" {
		return sessionEndTarget{}, p.messages.Failure(language, errSessionEndDetached, errSessionEndDetached.key, clitext.Params{clitext.ParamName: escapeLine(name)})
	}
	repo, err := prOriginRepo(ctx, dir)
	if err != nil {
		return sessionEndTarget{}, p.failure(language, err)
	}
	head, err := prGit(ctx, dir, nil, "rev-parse", "HEAD")
	if err != nil {
		return sessionEndTarget{}, p.failure(language, err)
	}
	squashed := mergedAtHead(record, head)
	if refusal := p.refuseUnsaved(ctx, language, name, branch, dir, squashed); refusal != nil {
		return sessionEndTarget{}, refusal
	}
	common, err := prGit(ctx, dir, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return sessionEndTarget{}, p.failure(language, err)
	}
	env, err := forge.GitEnvironment(ctx, p.credentials, repo.Host())
	if err != nil {
		return sessionEndTarget{}, p.failure(language, err)
	}
	return sessionEndTarget{language: language, record: record, branch: branch, common: common, env: env, squashed: squashed}, nil
}

func mergedAtHead(record state.PullRequestRecord, head string) bool {
	return record.State == contracts.PullRequestMerged && head == record.HeadSHA
}

func (p sessionPRs) refuseUnsaved(ctx context.Context, language i18n.Language, name, branch, dir string, saved bool) error {
	status, err := prGit(ctx, dir, nil, "status", "--porcelain")
	if err != nil {
		return p.failure(language, err)
	}
	if status != "" {
		return p.messages.Failure(language, errSessionEndDirty, errSessionEndDirty.key, clitext.Params{clitext.ParamName: escapeLine(name)})
	}
	if saved {
		return nil
	}
	unpushed, err := prGit(ctx, dir, nil, "rev-list", "--count", "HEAD", "--not", "--remotes")
	if err != nil {
		return p.failure(language, err)
	}
	if unpushed != "0" {
		return p.messages.Failure(language, errSessionEndUnpushed, errSessionEndUnpushed.key, clitext.Params{clitext.ParamBranch: escapeLine(branch)})
	}
	return nil
}

func (p sessionPRs) endOutcome(ctx context.Context, name string, target sessionEndTarget) (contracts.Result, error) {
	branch := clitext.Params{clitext.ParamBranch: escapeLine(target.branch)}
	closed, err := p.messages.Text(target.language, clitext.SessionEnded, clitext.Params{clitext.ParamName: escapeLine(name)})
	if err != nil {
		return contracts.Result{}, err
	}
	var remoteKey clitext.Key = clitext.SessionEndRemoteKept
	if target.record.State == contracts.PullRequestMerged {
		if err = deleteRemoteBranch(ctx, target); err != nil {
			return contracts.Result{}, p.messages.Failure(target.language, err, clitext.SessionEndRemoteFailed, clitext.Params{clitext.ParamBranch: escapeLine(target.branch), clitext.ParamErr: escapeLine(err.Error())})
		}
		remoteKey = clitext.SessionEndRemoteDeleted
	}
	remote, err := p.messages.Text(target.language, remoteKey, branch)
	if err != nil {
		return contracts.Result{}, err
	}
	var localKey clitext.Key = clitext.SessionEndLocalDeleted
	deleteFlag := "-d"
	if target.squashed {
		deleteFlag = "-D"
	}
	if _, err = prGit(ctx, target.common, nil, "--git-dir="+target.common, "branch", deleteFlag, target.branch); err != nil {
		localKey = clitext.SessionEndLocalKept
	}
	local, err := p.messages.Text(target.language, localKey, branch)
	if err != nil {
		return contracts.Result{}, err
	}
	return cli.Data(strings.Join([]string{closed, remote, local}, "\n"), nil)
}

func deleteRemoteBranch(ctx context.Context, target sessionEndTarget) error {
	listed, err := prGit(ctx, target.common, target.env, "--git-dir="+target.common, "ls-remote", "--heads", originRemote, "refs/heads/"+target.branch)
	if err != nil {
		return err
	}
	if listed == "" {
		return nil
	}
	_, err = prGit(ctx, target.common, target.env, "--git-dir="+target.common, "push", "--quiet", originRemote, "--delete", target.branch)
	return err
}
