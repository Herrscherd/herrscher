package manager

import (
	"context"
	"errors"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type createRequest struct {
	name, base string
	home       state.HomeRef
	admin      channelAdmin
	adopted    string
	runtime    runtimeChoice
	options    sessionOptions
	hostName   string
	account    string
	notice     *contracts.Notice
	mode       string
	shared     bool
	lang       string
}

func (h *Handler) sessionCreateRun(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	req, err := h.parseCreateRequest(ctx, input)
	if err != nil {
		return contracts.Result{}, err
	}
	if readyErr := h.checkHostReady(req.hostName); readyErr != nil {
		return contracts.Result{}, readyErr
	}
	place, err := h.placeSession(ctx, input, &req)
	if err != nil {
		return contracts.Result{}, err
	}
	approvalsNote := h.approvalsNote(ctx, req, place.space)
	runDir := place.space.path
	if runDir == "" {
		runDir = place.repo
	}
	sess := newSessionRecord(req, place.space, place.project, runDir)
	slot, err := h.openChannel(ctx, req, h.st.QualifiedName(req.name))
	if err != nil {
		if _, unsupported := errors.AsType[*UnsupportedHomeTypeError](err); unsupported {
			return contracts.Result{}, err
		}
		h.rollbackPlacement(ctx, place, req.name)
		return contracts.Result{}, err
	}
	sess.ChannelID, sess.Owned, sess.Type = slot.id, slot.owned, slot.typ
	sess.Approvals = req.mode
	if err := h.persisted(func() error { return h.st.AddSession(sess) }); err != nil {
		h.rollbackPlacement(ctx, place, req.name)
		if note := h.tidyChannel(ctx, sess); note != "" {
			h.log.Warn("channel rollback", "session", req.name, "note", note)
		}
		return contracts.Result{}, h.labelled("persist", clitext.HostErrPersist, err)
	}
	h.warnIfFailed(h.sup.StartWithin(sess, supervisorGrace), "start "+req.name)
	banner := h.sessionBanner(h.noticeLanguage(req.lang), place.repo, req.name, place.space.path, place.wt.Branch(req.name), req.runtime.cmd, req.runtime.model, req.shared)
	h.warnIfFailed(req.admin.Send(ctx, sess.ChannelID, banner), "send banner for "+req.name)
	h.announceAccount(ctx, req, sess.ChannelID)
	return contracts.DataResult(h.text(clitext.SessionRunning, clitext.Params{clitext.ParamChannel: req.admin.ChannelRef(sess.ChannelID), clitext.ParamBanner: banner, clitext.ParamApprovals: approvalsNote})), nil
}

func (h *Handler) parseCreateRequest(ctx context.Context, input contracts.Input) (createRequest, error) {
	raw, ok := input.Lookup(nameParam)
	if !ok {
		return createRequest{}, h.localized(ErrMissingName, clitext.HostErrMissingName)
	}
	name := slugify(raw)
	if name == "" || !sessionNameRe.MatchString(name) {
		return createRequest{}, &InvalidValueError{Field: nameParam, Value: raw, Hint: "use letters, digits, - or _ (max 64, no /, spaces or ..)"}
	}
	if _, exists := h.st.FindSession(name); exists {
		return createRequest{}, &SessionExistsError{Name: name}
	}
	if n := len(h.st.SnapshotSessions()); n >= maxSessions {
		return createRequest{}, &SessionLimitError{Max: maxSessions}
	}
	terminalOnly := input.Bool("terminal_only")
	adopted, _ := input.Lookup("channel_id")
	home, admin, err := h.chooseHome(ctx, input, adopted, terminalOnly)
	if err != nil {
		return createRequest{}, err
	}
	runtime, err := h.chooseRuntime(input)
	if err != nil {
		return createRequest{}, err
	}
	options, err := h.parseSessionOptions(input, terminalOnly)
	if err != nil {
		return createRequest{}, err
	}
	explicitAccount, _ := input.Lookup("account")
	hostName, planned, err := h.checkSessionAccount(input, runtime)
	if err != nil {
		return createRequest{}, err
	}
	if cycleErr := checkParentCycle(name, options.parent, h.st.SnapshotSessions()); cycleErr != nil {
		return createRequest{}, cycleErr
	}
	mode := strings.TrimSpace(input.Get("approvals"))
	if modeErr := validateApprovalsMode(mode); modeErr != nil {
		return createRequest{}, modeErr
	}
	base, _ := input.Lookup("base")
	lang, err := parseSessionLanguage(input)
	if err != nil {
		return createRequest{}, err
	}
	chosenAccount := explicitAccount != "" || h.agentAccount(runtime.agent) != ""
	account, accountNotice := h.balanceAccount(ctx, runtime, hostName, planned, chosenAccount)
	return createRequest{
		lang:     lang,
		name:     name,
		base:     base,
		home:     home,
		admin:    admin,
		adopted:  adopted,
		runtime:  runtime,
		options:  options,
		hostName: hostName,
		account:  account,
		notice:   accountNotice,
		mode:     mode,
		shared:   input.Bool("shared"),
	}, nil
}

func (h *Handler) chooseHome(ctx context.Context, input contracts.Input, adopted string, terminalOnly bool) (state.HomeRef, channelAdmin, error) {
	home, admin := h.st.Home, h.d
	if terminalOnly && h.td != nil {
		home = state.HomeRef{ID: terminalPlace, Type: terminalPlace}
		admin = h.td
	}
	under, ok := input.Lookup("under")
	if !ok || under == "" {
		if home.ID == "" && adopted == "" {
			return state.HomeRef{}, nil, ErrNoHome
		}
		return home, admin, nil
	}
	if adopted != "" {
		return state.HomeRef{}, nil, h.localized(ErrConflictingChannel, clitext.HostErrConflictingChannel)
	}
	if terminalOnly {
		return state.HomeRef{}, nil, h.localized(ErrConflictingPlacement, clitext.HostErrConflictingPlacement)
	}
	kind, err := admin.Kind(ctx, under)
	if err != nil {
		return state.HomeRef{}, nil, h.labelled("inspect channel", clitext.HostErrInspectChannel, err)
	}
	if kind != categoryKind && kind != forumKind {
		return state.HomeRef{}, nil, &ChannelKindError{Channel: under, Kind: kind, Need: "under must be a category or forum (to run in an existing conversation, pass channel_id)"}
	}
	return state.HomeRef{ID: under, Type: kind}, admin, nil
}

func newSessionRecord(req createRequest, space workspace, project, runDir string) state.Session {
	runtime, options := req.runtime, req.options
	return state.Session{
		Name: req.name, Cmd: runtime.cmd, Backend: runtime.backend, Vendor: runtime.vendor, ModelID: runtime.model,
		Worktree: space.path, Scratch: space.scratch, Dir: runDir, Project: project, Agent: runtime.agent,
		MemoryProject: options.memProject, MemoryAgent: options.memAgent, ProjectPinned: options.projectPinned,
		Parent: options.parent, Gateways: options.gateways, Extractor: options.extractor, Journal: options.journal,
		ConsolidateEvery: options.consolidateEvery, CostCap: options.caps.cost, TokenCap: options.caps.tokens,
		CohortCostCap: options.caps.cohortCost, CohortTokenCap: options.caps.cohortTokens, Host: req.hostName,
		Account: req.account, Language: req.lang,
	}
}
