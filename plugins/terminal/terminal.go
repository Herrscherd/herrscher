// Package terminal is the terminal gateway plugin: a chat gateway whose
// "channels" are tabs in the local TUI. It self-registers like any gateway (init →
// contracts.Register) and implements Gateway + ChannelReader + EventSink so the
// daemon hub drives it exactly like Discord — polling Read for typed lines and
// fanning the live event stream to it via Emit. The TUI binds to the active
// instance through Active().
package terminal

import (
	"context"
	"crypto/rand"
	"fmt"
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
	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

// ChannelID is the default conversation id used by the terminal gateway.
const ChannelID = "terminal"

func init() {
	contracts.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind:         "terminal",
			Category:     contracts.CategoryGateway,
			Capabilities: contracts.Capabilities{Replies: true},
		},
		Gateway: newGatewaySet,
	})
}

func newGatewaySet(ctx context.Context, cfg contracts.PluginConfig) (contracts.GatewaySet, error) {
	tm := New()
	return contracts.GatewaySet{Gateway: tm, Reader: tm, Admin: tm}, nil
}

// Terminal is the in-process terminal gateway. Typed lines arrive via Submit
// (tagged with the active channel) and are drained per-channel by the hub
// through Read; outbound events (EmitTo/Emit/Post/Reply) are forwarded to the
// TUI as RoutedEvents on Frontend.
type Terminal struct {
	mu      sync.Mutex
	pending map[string][]contracts.Message // channel id -> queued inbound lines
	nextID  int
	out     chan tui.RoutedEvent

	ctrlMu    sync.Mutex
	ctrl      contracts.SessionControl // set by BindSessionControl; nil-safe here
	ctrlReady chan struct{}            // closed once ctrl is bound
	bindOnce  sync.Once                // guards the close of ctrlReady

	baseCtx context.Context // the foreground lifetime; scopes operator dispatches

	// openSession / openText are what the window must open on when this process
	// is the one starting the daemon. They also suppress the default tab: a run
	// that already names the session it is for does not want a second, empty one.
	openSession string
	openText    string
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

// openDefaultSession answers which session a launching TUI opens on, and
// returns its name.
//
// Reopening the terminal means going back to what you were doing, so an
// existing terminal-bound session is reused — the one last spoken in, revived
// first if it had been archived. Only a host with no terminal session at all
// gets a new one. The old rule minted a session whenever none was *live*, which
// is how every restart after a close left one more tab nobody had asked for.
func openDefaultSession(ctx context.Context, c contracts.SessionControl) (string, error) {
	sessions := c.Sessions()
	if s, ok := lastTerminalSession(sessions); ok {
		if s.Archived {
			if err := c.Resume(s.Name); err != nil {
				return "", err
			}
		}
		return s.Name, nil
	}
	name := defaultSessionName(sessions)
	if _, err := c.Create(ctx, contracts.CreateSession{Name: name, TerminalOnly: true, Shared: true}); err != nil {
		return "", err
	}
	return name, nil
}

// lastTerminalSession picks the terminal-bound session a launch reopens: the one
// whose transcript was written to most recently, falling back to the last the
// daemon lists — the newest — when nothing has ever been said in any of them.
func lastTerminalSession(sessions []contracts.SessionInfo) (contracts.SessionInfo, bool) {
	var best contracts.SessionInfo
	found := false
	for _, s := range sessions {
		terminal := false
		for _, g := range s.Gateways {
			if g == "terminal" {
				terminal = true
				break
			}
		}
		if !terminal {
			continue
		}
		if !found || s.LastTs >= best.LastTs {
			best, found = s, true
		}
	}
	return best, found
}

// sessionNameRe mirrors the guard in core/internal/manager/validate.go: a name
// becomes a filesystem path and a git ref, so a directory whose name does not
// pass it cannot be one.
var sessionNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// defaultSessionName mints the name of the tab a first launch opens: the
// directory herrscher was started in, because that is what the operator calls
// what they are working on. A random name is the fallback for a directory whose
// name is not a legal session slug, or one another session already answers to —
// `session create` refuses a name already taken, and a refused create is a TUI
// that opens on nothing.
func defaultSessionName(taken []contracts.SessionInfo) string {
	wd, err := os.Getwd()
	if err != nil {
		return randomSessionName()
	}
	name := slug(filepath.Base(wd))
	if !sessionNameRe.MatchString(name) {
		return randomSessionName()
	}
	for _, s := range taken {
		if s.Name == name {
			return randomSessionName()
		}
	}
	return name
}

func randomSessionName() string {
	return "s-" + strings.ToLower(rand.Text()[:4])
}

// bootstrapSession waits for the host to bind SessionControl (RunHub binds it
// from a background goroutine after the TUI may have started), then resolves the
// session the window opens on. It blocks on the ctrlReady signal rather than
// polling, so a slow bind is picked up the instant it lands. Never blocks
// forever: if the bind doesn't arrive within ~5 s, or ctx is cancelled, it
// returns "" so the TUI still launches — a bootstrap is best-effort and must not
// stop it.
func (t *Terminal) bootstrapSession(ctx context.Context) string {
	select {
	case <-ctx.Done():
		return ""
	case <-time.After(5 * time.Second):
		return ""
	case <-t.ctrlReady:
		c := t.Control()
		if c == nil {
			return ""
		}
		name, err := openDefaultSession(ctx, c)
		if err != nil {
			return "" // best-effort; the window still opens, on whatever is there
		}
		return name
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
	// A run opened on a named session already knows its tab; bootstrapping is for
	// the plain launch, which has to work out which session it comes back to.
	if t.openSession == "" {
		t.openSession = t.bootstrapSession(ctx)
	}
	if t.openSession == "" {
		return tui.Run(ctx, cancel, t)
	}
	return tui.Run(ctx, cancel, t, tui.OpenOn(t.openSession, t.openText))
}

// New builds an unbound terminal gateway.
func New() *Terminal {
	return &Terminal{
		pending:   map[string][]contracts.Message{},
		out:       make(chan tui.RoutedEvent, 256),
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
	for _, a := range attachments {
		// Build the file URL through url.URL so a path with spaces, '#', or '?'
		// (all legal in a filename) is percent-encoded and round-trips intact —
		// a raw "file://"+path concat would truncate at the first '#'/'?'.
		fileURL := (&url.URL{Scheme: "file", Path: a.Path}).String()
		msg.Attachments = append(msg.Attachments, contracts.Attachment{
			Filename:    a.Name,
			URL:         fileURL,
			ContentType: a.Mime,
			Size:        int(a.Size),
		})
	}
	t.pending[channel] = append(t.pending[channel], msg)
}

// Frontend yields routed outbound events for the TUI to render.
func (t *Terminal) Frontend() <-chan tui.RoutedEvent { return t.out }

// --- contracts.ChannelReader ---

func (t *Terminal) Enabled() bool          { return true }
func (t *Terminal) DefaultChannel() string { return ChannelID }

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

func (t *Terminal) Unreact(context.Context, string, string, string) error { return nil }

func (t *Terminal) UpsertStatusMessage(_ context.Context, channelID, _, content string) (string, error) {
	t.EmitTo(contracts.Conversation{Gateway: "terminal", ID: channelID}, contracts.Event{T: "status", Text: content})
	return "", nil
}

// --- contracts.Gateway ---

func (t *Terminal) Manifest() contracts.Manifest {
	return contracts.Manifest{Kind: "terminal", Category: contracts.CategoryGateway}
}

func (t *Terminal) Post(_ context.Context, conv contracts.Conversation, text string) (contracts.MessageID, error) {
	t.EmitTo(conv, contracts.Event{T: "reply", Text: text, Done: true})
	return "", nil
}

func (t *Terminal) Reply(_ context.Context, conv contracts.Conversation, _ contracts.MessageID, text string) (contracts.MessageID, error) {
	t.EmitTo(conv, contracts.Event{T: "reply", Text: text, Done: true})
	return "", nil
}

func (t *Terminal) React(context.Context, contracts.Conversation, contracts.MessageID, string) error {
	return nil
}

func (t *Terminal) Menu(_ context.Context, conv contracts.Conversation, _ contracts.MessageID, prompt string, opts []contracts.Choice) error {
	text := prompt
	for _, o := range opts {
		text += "\n  • " + o.Value + " — " + o.Label
	}
	t.EmitTo(conv, contracts.Event{T: "status", Text: text})
	return nil
}

// --- contracts.RoutedEventSink ---

// EmitTo routes a turn event to the conversation's tab in the TUI.
func (t *Terminal) EmitTo(conv contracts.Conversation, e contracts.Event) {
	t.emit(tui.RoutedEvent{Conv: conv, Event: e})
}

// --- contracts.EventSink ---

// Emit (legacy EventSink) routes to the default single channel. The hub calls
// this when it sees only EventSink; RoutedEventSink takes priority.
func (t *Terminal) Emit(e contracts.Event) {
	t.emit(tui.RoutedEvent{Conv: contracts.Conversation{Gateway: "terminal", ID: ChannelID}, Event: e})
}

func (t *Terminal) emit(re tui.RoutedEvent) {
	select {
	case t.out <- re:
		return
	default:
	}
	// High-volume chunk/status events are dropped rather than block the hub when
	// the TUI lags. A finished reply or a channel close carries terminal state
	// (clears the busy marker, removes the tab) the TUI must not miss, so wait
	// briefly for room before giving up.
	if re.Event.T == "closed" || (re.Event.T == "reply" && re.Event.Done) {
		select {
		case t.out <- re:
		case <-time.After(2 * time.Second):
		}
	}
}

// Sessions returns the hub's sessions for tab labels (nil until SessionControl
// is bound — see BindSessionControl).
func (t *Terminal) Sessions() []contracts.SessionInfo {
	t.ctrlMu.Lock()
	c := t.ctrl
	t.ctrlMu.Unlock()
	if c == nil {
		return nil
	}
	return c.Sessions()
}

// Scrollback returns a session's recorded history lines through the bound
// SessionControl (nil until bound), so a reopened tab can seed its scrollback.
func (t *Terminal) Scrollback(name string) []contracts.ScrollbackLine {
	if c := t.Control(); c != nil {
		return c.Scrollback(name)
	}
	return nil
}

// Resume revives an archived session by name through the bound SessionControl,
// for the /resume picker.
func (t *Terminal) Resume(name string) (string, error) {
	c := t.Control()
	if c == nil {
		return "", fmt.Errorf("no session control")
	}
	if err := c.Resume(name); err != nil {
		return "", err
	}
	return "resumed " + name, nil
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
func (t *Terminal) BindSessionControl(c contracts.SessionControl) {
	t.ctrlMu.Lock()
	t.ctrl = c
	t.ctrlMu.Unlock()
	t.bindOnce.Do(func() { close(t.ctrlReady) })
}

// Control exposes the bound SessionControl to the TUI (nil before bind).
func (t *Terminal) Control() contracts.SessionControl {
	t.ctrlMu.Lock()
	defer t.ctrlMu.Unlock()
	return t.ctrl
}

// withTerminalDefault ensures that a "session create" command without an
// explicit gateway selector binds to the terminal, so TUI-created sessions
// always appear as tabs. An explicit --gateways or --terminal_only flag is
// respected and passed through unchanged.
func withTerminalDefault(args []string) []string {
	if len(args) < 2 || args[0] != "session" || args[1] != "create" {
		return args
	}
	for _, a := range args {
		if a == "--gateways" || strings.HasPrefix(a, "--gateways=") ||
			a == "--terminal_only" || strings.HasPrefix(a, "--terminal_only=") ||
			strings.HasPrefix(a, "terminal_only:") {
			return args
		}
	}
	out := make([]string, len(args), len(args)+1)
	copy(out, args)
	return append(out, "--terminal_only")
}

// terminalVerbs is the allow-list of command groups reachable from the TUI. The
// terminal drives session lifecycle and its companion agents, reads and edits
// the memory vault, and reads the model catalog. Daemon-management verbs are
// deliberately out of reach — service restart/update would tear down the host
// the TUI runs in, and set home/source rewrites its routing config — so a
// future destructive verb in either group can never be typed into a tab.
//
// The gate is by group, not by command, which is why membership has to be
// judged on the whole group: memory's most destructive verb is `forget`, and
// `memory restore` undoes it; models only lists.
var terminalVerbs = map[string]bool{
	"session": true,
	"agent":   true,
	"memory":  true,
	"models":  true,
}

// OperatorVerb reports whether a command's leading verb is reachable from a
// terminal frontend. It is exported because the attached frontend talks to the
// same daemon over a socket and must draw the same palette — two copies of this
// judgement would be two chances to disagree about what a tab may run.
func OperatorVerb(verb string) bool { return terminalVerbs[verb] }

// allowedVerbs lists the allow-list for an error an operator has to act on.
func allowedVerbs() string {
	out := make([]string, 0, len(terminalVerbs))
	for v := range terminalVerbs {
		out = append(out, v)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// Commands is the palette advertised to the TUI: the daemon's own registry,
// filtered to the verbs Dispatch accepts. Asking the registry rather than
// re-typing a list here is what keeps the two in step — the hand-kept version
// offered seven of the twenty-odd commands the terminal would have run.
//
// A registry that cannot be reached (control not bound yet) yields no palette
// rather than a stale one: the local verbs still stand on their own.
func (t *Terminal) Commands() []tui.CommandSpec {
	t.ctrlMu.Lock()
	c, ctx := t.ctrl, t.baseCtx
	t.ctrlMu.Unlock()
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Straight to the control seam, not through t.Dispatch: the gate below is
	// about what an operator may type, and this is the frontend asking what the
	// gate will let through.
	out, err := c.Dispatch(ctx, []string{"commands", "--json"})
	if err != nil {
		return nil
	}
	specs, err := tui.ParseCommandSpecs([]byte(out), func(verb string) bool { return terminalVerbs[verb] })
	if err != nil {
		return nil
	}
	return specs
}

// Dispatch forwards an operator argv to the bound SessionControl, prepending
// --terminal_only when creating a session without an explicit gateway selector
// so TUI-created sessions bind to this terminal and appear as tabs. The first
// token is gated against terminalVerbs before it ever reaches the registry.
func (t *Terminal) Dispatch(args []string) (string, error) {
	if len(args) == 0 || !terminalVerbs[args[0]] {
		verb := ""
		if len(args) > 0 {
			verb = args[0]
		}
		return "", fmt.Errorf("command %q is not available from the terminal (allowed: %s)", verb, allowedVerbs())
	}
	t.ctrlMu.Lock()
	c, ctx := t.ctrl, t.baseCtx
	t.ctrlMu.Unlock()
	if c == nil {
		return "", fmt.Errorf("session control not bound")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return c.Dispatch(ctx, withTerminalDefault(args))
}

// Close tears a session down through the typed SessionControl seam, so the TUI's
// close action never assembles "session close" flag argv.
func (t *Terminal) Close(name string, force bool) (string, error) {
	t.ctrlMu.Lock()
	c, ctx := t.ctrl, t.baseCtx
	t.ctrlMu.Unlock()
	if c == nil {
		return "", fmt.Errorf("session control not bound")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return c.Close(ctx, name, force)
}

// --- contracts.ChannelAdmin: synthetic, terminal-local channels ---

func (t *Terminal) Kind(_ context.Context, _ string) (string, error) { return "text", nil }

func (t *Terminal) CreateUnder(_ context.Context, _, name string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nextID++
	return "terminal/" + slug(name) + "-" + strconv.Itoa(t.nextID), nil
}

func (t *Terminal) ForumPost(ctx context.Context, parentID, name, _ string) (string, error) {
	return t.CreateUnder(ctx, parentID, name)
}

func (t *Terminal) Archive(_ context.Context, id string) error {
	t.EmitTo(contracts.Conversation{Gateway: "terminal", ID: id}, contracts.Event{T: "closed"})
	return nil
}

func (t *Terminal) Send(_ context.Context, channelID, content string) error {
	t.EmitTo(contracts.Conversation{Gateway: "terminal", ID: channelID}, contracts.Event{T: "status", Text: content})
	return nil
}

// ChannelRef renders a terminal channel id bare: a plain TUI has no mention
// markup, so the operator just sees the id.
func (t *Terminal) ChannelRef(id string) string { return id }

// slug lowercases and replaces unsafe runes so a channel id stays path-safe.
func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "session"
	}
	return out
}
