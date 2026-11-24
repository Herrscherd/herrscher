package manage

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/forgecreds"
	"github.com/Herrscherd/herrscher/core/forgeport"
	"github.com/Herrscherd/herrscher/core/host"
)

const gitHubEventBuffer = 2

type gitHubLink interface {
	status(ctx context.Context) (gitHubStatus, error)
	connect(ctx context.Context, show func(forgecreds.DeviceCode) error) error
}

type gitHubStatus struct {
	connected bool
	login     string
	source    string
}

type liveGitHub struct {
	creds *forgecreds.Credentials
	host  contracts.ForgeHost
}

func (link liveGitHub) status(ctx context.Context) (gitHubStatus, error) {
	token, err := link.creds.Token(ctx, link.host)
	if errors.Is(err, contracts.ErrForgeNotConnected) {
		return gitHubStatus{}, nil
	}
	if err != nil {
		return gitHubStatus{}, err
	}
	status := gitHubStatus{connected: true, source: token.Source().String()}
	if forge, openErr := forgeport.Open(ctx, forgeport.KindGitHub, link.host, link.creds); openErr == nil {
		if connection, connErr := forge.Connection(ctx); connErr == nil {
			status.login = connection.Login
		}
	}
	return status, nil
}

func (link liveGitHub) connect(ctx context.Context, show func(forgecreds.DeviceCode) error) error {
	return link.creds.Connect(ctx, link.host, show)
}

type gitHubState struct {
	status    gitHubStatus
	statusErr error
	known     bool
	busy      bool
	awaiting  bool
	code      forgecreds.DeviceCode
	failure   error
	cancel    context.CancelFunc
}

type gitHubStatusMsg struct {
	status gitHubStatus
	err    error
}

type gitHubCodeMsg struct {
	code   forgecreds.DeviceCode
	events <-chan tea.Msg
}

type gitHubConnectDoneMsg struct {
	err error
}

func (model *setupModel) githubStatus() tea.Cmd {
	link := model.deps.github
	if link == nil || model.github.busy {
		return nil
	}
	model.github.busy = true
	parent := model.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, probeTimeout)
		defer cancel()
		status, err := link.status(ctx)
		return gitHubStatusMsg{status: status, err: err}
	}
}

func (model *setupModel) connectGitHub() tea.Cmd {
	link := model.deps.github
	if link == nil || model.github.busy {
		return nil
	}
	ctx, cancel := context.WithCancel(model.ctx)
	model.github.cancel = cancel
	model.github.busy = true
	model.github.failure = nil
	events := make(chan tea.Msg, gitHubEventBuffer)
	go func() {
		err := link.connect(ctx, func(code forgecreds.DeviceCode) error {
			events <- gitHubCodeMsg{code: code, events: events}
			return nil
		})
		events <- gitHubConnectDoneMsg{err: err}
	}()
	return nextGitHubEvent(events)
}

func nextGitHubEvent(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-events }
}

func (model *setupModel) cancelGitHub() {
	if model.github.cancel != nil {
		model.github.cancel()
		model.github.cancel = nil
	}
}

func (model *setupModel) gitHubStatusDone(done gitHubStatusMsg) {
	model.github.busy = false
	model.github.known = done.err == nil
	model.github.statusErr = done.err
	model.github.status = done.status
}

func (model *setupModel) gitHubCodeShown(shown gitHubCodeMsg) tea.Cmd {
	model.github.awaiting = true
	model.github.code = shown.code
	return nextGitHubEvent(shown.events)
}

func (model *setupModel) gitHubConnectDone(done gitHubConnectDoneMsg) tea.Cmd {
	model.cancelGitHub()
	model.github.awaiting = false
	model.github.busy = false
	model.github.failure = done.err
	if done.err != nil {
		return nil
	}
	return model.githubStatus()
}

func (model *setupModel) githubRows() []setupRow {
	rows := []setupRow{line(toneGroup, model.text(clitext.SetupGitHubTitle, nil)), model.githubStateRow()}
	if model.github.awaiting {
		rows = append(rows, line(toneInfo, "  "+model.text(clitext.SetupGitHubCode, clitext.Params{clitext.ParamURL: model.github.code.VerificationURI, clitext.ParamUserCode: model.github.code.UserCode})))
	}
	if model.github.failure != nil {
		rows = append(rows, line(toneBad, "  ✗ "+model.gitHubFailureText(model.github.failure)))
	}
	return append(rows, action(model.text(clitext.SetupGitHubConnect, nil), model.text(clitext.SetupGitHubInstall, clitext.Params{clitext.ParamURL: forgecreds.AppInstallURL}), func(target *setupModel) tea.Cmd { return target.connectGitHub() }))
}

func (model *setupModel) githubStateRow() setupRow {
	state := model.githubState()
	return line(state.tone, "  "+state.text)
}

func (model *setupModel) githubState() sectionState {
	switch {
	case model.github.busy && !model.github.known:
		return sectionState{tone: toneDim, text: model.text(clitext.SetupProbeRunning, nil)}
	case model.github.statusErr != nil:
		return sectionState{tone: toneBad, text: model.text(clitext.SetupGitHubStatusFailed, clitext.Params{clitext.ParamErr: model.github.statusErr.Error()})}
	case !model.github.known:
		return sectionState{tone: toneDim, text: model.text(clitext.SetupProbeRunning, nil)}
	case !model.github.status.connected:
		return sectionState{tone: toneWarn, text: model.text(clitext.SetupGitHubNotConnected, nil)}
	}
	return sectionState{tone: toneOK, text: model.text(clitext.SetupGitHubConnected, clitext.Params{clitext.ParamLogin: model.gitHubLogin(), clitext.ParamSource: model.github.status.source})}
}

func (model *setupModel) gitHubLogin() string {
	if login := model.github.status.login; login != "" {
		return login
	}
	return model.text(clitext.ConnectLoginUnknown, nil)
}

func (model *setupModel) gitHubFailureText(err error) string {
	switch {
	case errors.Is(err, forgecreds.ErrDeviceDenied):
		return model.text(clitext.SetupGitHubDenied, nil)
	case errors.Is(err, forgecreds.ErrDeviceExpired):
		return model.text(clitext.SetupGitHubExpired, nil)
	case errors.Is(err, forgecreds.ErrAppUnavailable):
		return model.text(clitext.SetupGitHubAppUnavailable, nil)
	}
	return model.text(clitext.SetupGitHubFailed, clitext.Params{clitext.ParamErr: err.Error()})
}

func (model *setupModel) connectionRows() []setupRow {
	return append(model.githubRows(),
		blank(),
		action(model.text(clitext.SetupConnectionsSkip, nil), "", func(target *setupModel) tea.Cmd { return target.back() }),
	)
}

func liveGitHubLink() gitHubLink {
	parsed, err := contracts.ParseForgeHost(forgeport.GitHubHost)
	if err != nil {
		return nil
	}
	return liveGitHub{creds: forgecreds.New(host.DefaultStatePath()), host: parsed}
}

func (model *setupModel) connectionsSection() hubSection {
	state := model.githubState()
	return hubSection{name: model.text(clitext.SetupSectionConnections, nil), state: state.text, screen: screenConnections, tone: state.tone}
}
