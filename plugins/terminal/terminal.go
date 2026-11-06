// Package terminal is the terminal gateway plugin: a chat gateway whose
// "channels" are tabs in the local TUI. It self-registers like any gateway (init →
// contracts.Register) and implements Gateway + ChannelReader + EventSink so the
// daemon hub drives it exactly like Discord, polling Read for typed lines and
// fanning the live event stream to it via Emit. The TUI binds to the active
// instance through Active().
package terminal

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/language"
	"github.com/Herrscherd/herrscher/core/scope"
	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

const (
	routeEventBufferSize = 256
	frontendCloseWait    = 2 * time.Second
	readOnlyCommandWords = 2
)

var (
	errNoSessionControl         = errors.New("terminal.no_session_control")
	errNotAvailableFromTerminal = errors.New("terminal.not_available")
	errSessionControlUnbound    = errors.New("terminal.session_unbound")
)

// ChannelID is the channel the terminal gateway serves when none is named.
const ChannelID = "terminal"

// Kind is what this gateway is called wherever gateways are named. A session's
// bindings, a manifest, the origin an attached window claims when it speaks into
// a session it is only watching.
const Kind = "terminal"

const (
	eventStatus = "status"
	eventReply  = "reply"
	eventMenu   = "menu"
	eventNotice = "notice"
	sessionVerb = "session"
)

func settings() []contracts.Setting { return scope.LaunchSettings() }

func newGatewaySet(_ context.Context, cfg contracts.PluginConfig) (contracts.GatewaySet, error) {
	term := New()
	term.cfg = cfg
	return contracts.GatewaySet{Gateway: term, Reader: term, Admin: term}, nil
}

// Terminal is the in-process terminal gateway. Typed lines arrive via Submit
// (tagged with the active channel) and are drained per-channel by the hub
// through Read; outbound events (EmitTo/Emit/Post/Reply) are forwarded to the
// TUI as RoutedEvents on Frontend.
type Terminal struct {
	mu      sync.Mutex
	cfg     contracts.PluginConfig
	pending map[string][]contracts.Message
	nextID  int
	out     chan tui.RoutedEvent

	ctrlMu    sync.Mutex
	ctrl      contracts.SessionControl
	ctrlReady chan struct{}
	bindOnce  sync.Once

	baseCtx context.Context

	openSession string
	openText    string

	mintedSession string
}

// OpenOn tells the gateway which session its window opens on, and what to send
// there first. The composition root calls it before the foreground starts; the
// host reaches it through a structural interface, so no core package learns that
// a terminal exists.
func (t *Terminal) OpenOn(session, text string) {
	t.openSession, t.openText = session, text
}

var (
	_ contracts.Gateway                = (*Terminal)(nil)
	_ contracts.ChannelReader          = (*Terminal)(nil)
	_ contracts.EventSink              = (*Terminal)(nil)
	_ contracts.RoutedEventSink        = (*Terminal)(nil)
	_ contracts.Foreground             = (*Terminal)(nil)
	_ contracts.ChannelAdmin           = (*Terminal)(nil)
	_ contracts.SessionControlReceiver = (*Terminal)(nil)
)

func openDefaultSession(ctx context.Context, control contracts.SessionControl, cfg contracts.PluginConfig) (string, error) {
	name := defaultSessionName(control.Sessions())
	spec := contracts.CreateSession{Name: name, TerminalOnly: true, Shared: true}
	scope.LaunchFrom(ctx, cfg).Apply(&spec)
	if _, err := control.Create(ctx, spec); err != nil {
		return "", err
	}
	return name, nil
}

var sessionNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func defaultSessionName(taken []contracts.SessionInfo) string {
	workdir, err := os.Getwd()
	if err != nil {
		return randomSessionName()
	}
	base := slug(filepath.Base(workdir))
	if !sessionNameRe.MatchString(base) {
		return randomSessionName()
	}
	used := make(map[string]bool)
	for i := range taken {
		used[taken[i].Name] = true
	}
	for count := 1; count <= 9; count++ {
		name := base
		if count > 1 {
			name = fmt.Sprintf("%s-%d", base, count)
		}
		if !used[name] && sessionNameRe.MatchString(name) {
			return name
		}
	}
	return randomSessionName()
}

func randomSessionName() string {
	return "s-" + strings.ToLower(rand.Text()[:4])
}

const bootstrapBudget = 5 * time.Second

func (t *Terminal) bootstrapSession(ctx context.Context) string {
	select {
	case <-ctx.Done():
		return ""
	case <-time.After(bootstrapBudget):
		return ""
	case <-t.ctrlReady:
		control := t.Control()
		if control == nil {
			return ""
		}
		cctx, cancel := context.WithTimeout(ctx, bootstrapBudget)
		defer cancel()
		name, err := openDefaultSession(cctx, control, t.cfg)
		if err != nil {
			return ""
		}
		t.mintedSession = name
		return name
	}
}

func (t *Terminal) archiveIfUntouched() {
	name := t.mintedSession
	if name == "" {
		return
	}
	t.mintedSession = ""
	t.ctrlMu.Lock()
	control, ctx := t.ctrl, t.baseCtx
	t.ctrlMu.Unlock()
	if control == nil || ctx == nil || ctx.Err() != nil {
		return
	}
	sessions := control.Sessions()
	for i := range sessions {
		session := &sessions[i]
		if session.Name != name {
			continue
		}
		if session.Archived || session.LastTs != "" {
			return
		}
		_, err := control.Close(ctx, name, false)
		bestEffort(err)
		return
	}
}

// RunForeground satisfies contracts.Foreground: the terminal gateway owns the
// process's main thread by running its Bubbletea TUI, blocking until the user
// quits (which calls cancel to tear the daemon down) or ctx is cancelled. The
// composition root runs this for the one bound gateway that implements
// Foreground, on an interactive TTY only.
func (t *Terminal) RunForeground(ctx context.Context, cancel context.CancelFunc) error {
	t.ctrlMu.Lock()
	t.baseCtx = ctx
	t.ctrlMu.Unlock()
	if t.openSession == "" {
		t.openSession = t.bootstrapSession(ctx)
	}
	quit := func() {
		t.archiveIfUntouched()
		cancel()
	}
	if t.openSession == "" {
		return tui.Run(ctx, quit, t, tui.Language(clientLanguage()))
	}
	return tui.Run(ctx, quit, t, tui.Language(clientLanguage()), tui.OpenOn(t.openSession, t.openText))
}

// New builds an unbound terminal gateway.
func New() *Terminal {
	return &Terminal{
		pending:   make(map[string][]contracts.Message),
		out:       make(chan tui.RoutedEvent, routeEventBufferSize),
		ctrlReady: make(chan struct{}),
	}
}

// Submit enqueues a line the user typed in the TUI as an inbound message on the
// given channel (the active tab's session channel). Staged attachments are carried
// as file:// URLs so the host bridge reads them off local disk instead of a CDN.
func (t *Terminal) Submit(channel, text string, attachments []tui.Attachment) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nextID++
	msg := contracts.Message{
		ID:         "t" + strconv.Itoa(t.nextID),
		ChannelID:  channel,
		Content:    text,
		AuthorID:   "local",
		AuthorName: "you",
	}
	for _, attachment := range attachments {
		fileURL := (&url.URL{Scheme: "file", Path: attachment.Path}).String()
		msg.Attachments = append(msg.Attachments, contracts.Attachment{
			Filename:    attachment.Name,
			URL:         fileURL,
			ContentType: attachment.Mime,
			Size:        int(attachment.Size),
		})
	}
	t.pending[channel] = append(t.pending[channel], msg)
}

// Frontend yields routed outbound events for the TUI to render.
func (t *Terminal) Frontend() <-chan tui.RoutedEvent { return t.out }

// Enabled reports that the terminal gateway is available whenever it is bound.
func (t *Terminal) Enabled() bool { return true }

// DefaultChannel returns the channel that messages without one are routed to.
func (t *Terminal) DefaultChannel() string { return ChannelID }

// EnsureChannel returns the single terminal channel, which always exists.
func (t *Terminal) EnsureChannel(context.Context, string, string) (contracts.Channel, error) {
	return contracts.Channel{ID: ChannelID, Name: ChannelID}, nil
}

// Read drains and returns the lines queued for channelID since the last Read.
// The hub polls this per-session with the session's own channel, so each
// session drains only its own input.
func (t *Terminal) Read(_ context.Context, channelID string, _ int, _ string) ([]contracts.Message, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := t.pending[channelID]
	if len(out) == 0 {
		return nil, nil
	}
	delete(t.pending, channelID)
	return out, nil
}

// Unreact is a no-op: the terminal shows no reactions.
func (t *Terminal) Unreact(context.Context, string, string, string) error { return nil }

// UpsertStatusMessage shows content as a status line in the channel tab and returns no message id.
func (t *Terminal) UpsertStatusMessage(_ context.Context, channelID, _, content string) (string, error) {
	t.EmitTo(contracts.Conversation{Gateway: Kind, ID: channelID}, contracts.Event{T: eventStatus, Text: content})
	return "", nil
}

// Manifest describes the terminal as a gateway plugin.
func (t *Terminal) Manifest() contracts.Manifest {
	return contracts.Manifest{Kind: Kind, Category: contracts.CategoryGateway}
}

// Post emits text as a finished reply in the conversation and returns no message id.
func (t *Terminal) Post(_ context.Context, conv contracts.Conversation, text string) (contracts.MessageID, error) {
	t.EmitTo(conv, contracts.Event{T: eventReply, Text: text, Done: true})
	return "", nil
}

// Reply emits text as a finished reply in the conversation and returns no message id.
func (t *Terminal) Reply(_ context.Context, conv contracts.Conversation, _ contracts.MessageID, text string) (contracts.MessageID, error) {
	t.EmitTo(conv, contracts.Event{T: eventReply, Text: text, Done: true})
	return "", nil
}

// Notify renders the notice in lang and shows it as a notice line of the conversation's tab.
func (t *Terminal) Notify(_ context.Context, conv contracts.Conversation, lang i18n.Language, notice contracts.Notice) (contracts.MessageID, error) {
	text, err := contracts.NewRenderer().Render(lang, notice)
	if err != nil {
		return "", err
	}
	t.EmitTo(conv, contracts.Event{T: eventNotice, Text: text})
	return "", nil
}

// React is a no-op: the terminal does not render reactions.
func (t *Terminal) React(context.Context, contracts.Conversation, contracts.MessageID, string) error {
	return nil
}

type menuOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type menuPayload struct {
	Prompt  string       `json:"prompt"`
	Options []menuOption `json:"options"`
}

// Menu sends a prompt with its choices to the conversation as a menu the TUI
// draws, and mirrors the choices in the text of the message.
func (t *Terminal) Menu(_ context.Context, conv contracts.Conversation, _ contracts.MessageID, prompt string, opts []contracts.Choice) error {
	var text strings.Builder
	text.WriteString(prompt)
	payload := menuPayload{Prompt: prompt}
	for _, option := range opts {
		text.WriteString("\n  • " + option.Value + ": " + option.Label)
		payload.Options = append(payload.Options, menuOption{Label: option.Label, Value: option.Value})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	t.EmitTo(conv, contracts.Event{T: eventMenu, Text: text.String(), Value: string(encoded)})
	return nil
}

// EmitTo routes a turn event to the conversation's tab in the TUI.
func (t *Terminal) EmitTo(conv contracts.Conversation, e contracts.Event) {
	t.emit(tui.RoutedEvent{Conv: conv, Event: e})
}

// Emit (legacy EventSink) routes to the default single channel. The hub calls
// this when it sees only EventSink; RoutedEventSink takes priority.
func (t *Terminal) Emit(e contracts.Event) {
	t.emit(tui.RoutedEvent{Conv: contracts.Conversation{Gateway: Kind, ID: ChannelID}, Event: e})
}

func (t *Terminal) emit(routed tui.RoutedEvent) {
	select {
	case t.out <- routed:
		return
	default:
	}
	if routed.Event.T == "closed" || routed.Event.T == eventMenu || (routed.Event.T == eventReply && routed.Event.Done) {
		select {
		case t.out <- routed:
		case <-time.After(frontendCloseWait):
		}
	}
}

// Sessions returns the hub's sessions for tab labels (nil until SessionControl
// is bound, see BindSessionControl).
func (t *Terminal) Sessions() []contracts.SessionInfo {
	t.ctrlMu.Lock()
	control := t.ctrl
	t.ctrlMu.Unlock()
	if control == nil {
		return nil
	}
	return control.Sessions()
}

// Scrollback returns a session's recorded history lines through the bound
// SessionControl (nil until bound), so a reopened tab can seed its scrollback.
func (t *Terminal) Scrollback(name string) []contracts.ScrollbackLine {
	if c := t.Control(); c != nil {
		return c.Scrollback(name)
	}
	return nil
}

// Seen records that the operator is looking at a session, which clears its
// review mark on the sessions board once its last answer is read.
func (t *Terminal) Seen(name string) {
	if _, err := t.Dispatch([]string{sessionVerb, "seen", sessionNameFlag, name}); err != nil {
		slog.Debug("review mark not cleared", "session", name, "error", err)
	}
}

// Overview returns the host's per-session overview as JSON (state, cost, last
// activity) for the sessions board and the side-by-side panes.
func (t *Terminal) Overview() (string, error) {
	return t.Dispatch([]string{sessionVerb, "overview"})
}

// Resume revives an archived session by name through the bound SessionControl,
// for the /resume picker.
func (t *Terminal) Resume(name string) (string, error) {
	control := t.Control()
	if control == nil {
		return "", terminalFailure(errNoSessionControl, clitext.TerminalNoSessionControl, nil)
	}
	if err := control.Resume(name); err != nil {
		return "", err
	}
	return "resumed " + name, nil
}

// Pick answers the choice value of the session bound to channel, through the
// bound SessionControl. It reports false when no control is bound or no session
// owns the channel.
func (t *Terminal) Pick(channel, value string) bool {
	control := t.Control()
	if control == nil {
		return false
	}
	sessions := control.Sessions()
	for index := range sessions {
		if sessions[index].ChannelID == channel {
			return control.Pick(sessions[index].Name, value)
		}
	}
	return false
}

// Interrupt cancels a session's in-flight turn through the bound SessionControl,
// for esc-to-interrupt from the TUI. Reports false when no control is bound or
// no live session by that name is driving.
func (t *Terminal) Interrupt(name string) bool {
	if c := t.Control(); c != nil {
		return c.Interrupt(name)
	}
	return false
}

// BindSessionControl stores the hub controller so the TUI can drive the session
// lifecycle (create/close/list) and enumerate sessions for tab labels.
func (t *Terminal) BindSessionControl(control contracts.SessionControl) {
	t.ctrlMu.Lock()
	t.ctrl = control
	t.ctrlMu.Unlock()
	t.bindOnce.Do(func() { close(t.ctrlReady) })
}

// Control exposes the bound SessionControl to the TUI (nil before bind).
func (t *Terminal) Control() contracts.SessionControl {
	t.ctrlMu.Lock()
	defer t.ctrlMu.Unlock()
	return t.ctrl
}

func withTerminalDefault(args []string) []string {
	if len(args) < 2 || args[0] != sessionVerb || args[1] != "create" {
		return args
	}
	for _, arg := range args {
		if arg == "--gateways" || strings.HasPrefix(arg, "--gateways=") ||
			arg == "--terminal_only" || strings.HasPrefix(arg, "--terminal_only=") ||
			strings.HasPrefix(arg, "terminal_only:") {
			return args
		}
	}
	out := make([]string, len(args), len(args)+1)
	copy(out, args)
	return append(out, "--terminal_only")
}

const sessionNameFlag = "--name"

func terminalVerbs() map[string]bool {
	return map[string]bool{
		sessionVerb: true,
		"agent":     true,
		"memory":    true,
		"models":    true,
	}
}

// OperatorVerb reports whether a command's leading verb is reachable from a
// terminal frontend. It is exported because the attached frontend talks to the
// same daemon over a socket and must draw the same palette; two copies of this
// judgement would be two chances to disagree about what a tab may run.
func OperatorVerb(verb string) bool { return terminalVerbs()[verb] }

// ReadOnlyCommand reports whether argv is one of the read-only queries the
// sessions board runs beside its list (skills, schedules, approval rules).
func ReadOnlyCommand(args []string) bool {
	if len(args) != readOnlyCommandWords {
		return false
	}
	switch args[0] + " " + args[1] {
	case "skill list", "schedule list", "approve list", "approve rule":
		return true
	}
	return false
}

func allowedVerbs() string {
	out := make([]string, 0, len(terminalVerbs()))
	for v := range terminalVerbs() {
		out = append(out, v)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// Commands is the palette advertised to the TUI: the daemon's own registry,
// filtered to the verbs Dispatch accepts. Asking the registry rather than
// re-typing a list here is what keeps the two in step; the hand-kept version
// offered seven of the twenty-odd commands the terminal would have run.
//
// A registry that cannot be reached (control not bound yet) yields no palette
// rather than a stale one: the local verbs still stand on their own.
func (t *Terminal) Commands() []tui.CommandSpec {
	t.ctrlMu.Lock()
	call, ctx := t.ctrl, t.baseCtx
	t.ctrlMu.Unlock()
	if call == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	out, err := textResult(call.Dispatch(ctx, []string{"commands", "--json"}))
	if err != nil {
		return nil
	}
	specs, err := tui.ParseCommandSpecs([]byte(out), func(verb string) bool { return terminalVerbs()[verb] })
	if err != nil {
		return nil
	}
	return specs
}

// Dispatch forwards an operator argv to the bound SessionControl, prepending
// --terminal_only when creating a session without an explicit gateway selector
// so TUI-created sessions bind to this terminal and appear as tabs. The first
// token is gated against terminalVerbs() before it ever reaches the registry.
func (t *Terminal) Dispatch(args []string) (string, error) {
	if len(args) == 0 || (!terminalVerbs()[args[0]] && !ReadOnlyCommand(args)) {
		verb := ""
		if len(args) > 0 {
			verb = args[0]
		}
		return "", terminalFailure(errNotAvailableFromTerminal, clitext.TerminalNotAvailable, clitext.Params{clitext.ParamCommand: strconv.Quote(verb), clitext.ParamAllowed: allowedVerbs()})
	}
	t.ctrlMu.Lock()
	call, ctx := t.ctrl, t.baseCtx
	t.ctrlMu.Unlock()
	if call == nil {
		return "", terminalFailure(errSessionControlUnbound, clitext.TerminalSessionUnbound, nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return textResult(call.Dispatch(ctx, withTerminalDefault(args)))
}

// Close tears a session down through the typed SessionControl seam, so the TUI's
// close action never assembles "session close" flag argv.
func (t *Terminal) Close(name string, force bool) (string, error) {
	t.ctrlMu.Lock()
	control, ctx := t.ctrl, t.baseCtx
	t.ctrlMu.Unlock()
	if control == nil {
		return "", terminalFailure(errSessionControlUnbound, clitext.TerminalSessionUnbound, nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return textResult(control.Close(ctx, name, force))
}

// Kind reports every attachment as text, since the terminal has no richer kinds.
func (t *Terminal) Kind(_ context.Context, _ string) (string, error) { return "text", nil }

// CreateUnder names a new terminal conversation under parent, unique per process.
func (t *Terminal) CreateUnder(_ context.Context, _, name string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nextID++
	return "terminal/" + slug(name) + "-" + strconv.Itoa(t.nextID), nil
}

// ForumPost opens a terminal conversation named name under parentID.
func (t *Terminal) ForumPost(ctx context.Context, parentID, name, _ string) (string, error) {
	return t.CreateUnder(ctx, parentID, name)
}

// Archive reports the conversation id as closed to its tab.
func (t *Terminal) Archive(_ context.Context, id string) error {
	t.EmitTo(contracts.Conversation{Gateway: Kind, ID: id}, contracts.Event{T: "closed"})
	return nil
}

// Send shows content as a status line in the conversation channelID.
func (t *Terminal) Send(_ context.Context, channelID, content string) error {
	t.EmitTo(contracts.Conversation{Gateway: Kind, ID: channelID}, contracts.Event{T: eventStatus, Text: content})
	return nil
}

// ChannelRef renders a terminal channel id bare: a plain TUI has no mention
// markup, so the operator just sees the id.
func (t *Terminal) ChannelRef(id string) string { return id }

func slug(text string) string {
	var builder strings.Builder
	for _, char := range strings.ToLower(text) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9', char == '-', char == '_':
			builder.WriteRune(char)
		default:
			builder.WriteRune('-')
		}
	}
	out := strings.Trim(builder.String(), "-")
	if out == "" {
		return sessionVerb
	}
	return out
}

func bestEffort(_ error) {}

func textResult(res contracts.Result, err error) (string, error) {
	return cli.Render(res, err, clientLanguage())
}

func terminalFailure(cause error, key clitext.Key, params clitext.Params) error {
	return clitext.Failure(clientLanguage(), cause, key, params)
}

func clientLanguage() i18n.Language {
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		return language.ForClient("")
	}
	return language.ForClient(cfg.Language)
}
