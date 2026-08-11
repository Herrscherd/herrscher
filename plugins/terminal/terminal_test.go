package terminal

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

// The gateway the factory builds must expose the Foreground capability, since
// serve discovers the bound gateway to run on the main thread through that
// interface (not a concrete import). Guards against silently dropping it.
func TestGatewaySetExposesForeground(t *testing.T) {
	set, err := newGatewaySet(context.Background(), contracts.PluginConfig{})
	if err != nil {
		t.Fatalf("newGatewaySet: %v", err)
	}
	if _, ok := set.Gateway.(contracts.Foreground); !ok {
		t.Fatal("terminal gateway must implement contracts.Foreground")
	}
}

func TestMenuRendersChoices(t *testing.T) {
	tm := New()
	conv := contracts.Conversation{Gateway: "terminal", ID: "ch"}
	if err := tm.Menu(context.Background(), conv, "", "pick one", []contracts.Choice{
		{Label: "first", Value: "1"},
		{Label: "second", Value: "2"},
	}); err != nil {
		t.Fatal(err)
	}
	re := <-tm.Frontend()
	if !strings.Contains(re.Event.Text, "first") || !strings.Contains(re.Event.Text, "second") {
		t.Fatalf("menu must render its choices, got %q", re.Event.Text)
	}
}

func TestEmitDeliversControlEventUnderBackpressure(t *testing.T) {
	tm := New()
	// Fill the outbound buffer with droppable chunk events.
	for i := 0; i < cap(tm.out); i++ {
		tm.emit(tui.RoutedEvent{Event: contracts.Event{T: "chunk", Text: "x"}})
	}
	// A finished reply must still be delivered (not dropped); drain one slot in
	// parallel so the brief wait succeeds.
	go func() { <-tm.Frontend() }()
	done := make(chan struct{})
	go func() {
		tm.emit(tui.RoutedEvent{Event: contracts.Event{T: "reply", Done: true, Text: "final"}})
		close(done)
	}()
	<-done
}

func TestBootstrapWaitsForBindThenCreates(t *testing.T) {
	tm := New()
	fake := &fakeSessionControl{}
	got := make(chan string, 1)
	go func() { got <- tm.bootstrapSession(context.Background()) }()
	// Bind after a beat: the ready signal must wake the bootstrap immediately.
	tm.BindSessionControl(fake)
	var name string
	select {
	case name = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("bootstrap did not return after bind")
	}
	if len(fake.created) != 1 {
		t.Fatalf("bootstrap did not create a default session: %+v", fake.created)
	}
	if name != fake.created[0].Name {
		t.Fatalf("bootstrap returned %q, want the session it created (%q)", name, fake.created[0].Name)
	}
}

func TestReadDrainsPerChannel(t *testing.T) {
	tm := New()
	tm.Submit("chA", "hello", nil)
	tm.Submit("chB", "world", nil)

	a, _ := tm.Read(context.Background(), "chA", 100, "")
	if len(a) != 1 || a[0].Content != "hello" || a[0].ChannelID != "chA" {
		t.Fatalf("chA Read = %+v", a)
	}
	b, _ := tm.Read(context.Background(), "chB", 100, "")
	if len(b) != 1 || b[0].Content != "world" {
		t.Fatalf("chB Read = %+v", b)
	}
	if a2, _ := tm.Read(context.Background(), "chA", 100, ""); len(a2) != 0 {
		t.Fatalf("chA second Read = %+v, want empty", a2)
	}
}

func TestEmitToRoutesToFrontend(t *testing.T) {
	tm := New()
	got := make(chan tui.RoutedEvent, 1)
	go func() { got <- <-tm.Frontend() }()
	tm.EmitTo(contracts.Conversation{Gateway: "terminal", ID: "chX"}, contracts.Event{T: "chunk", Text: "a"})
	re := <-got
	if re.Conv.ID != "chX" || re.Event.Text != "a" {
		t.Fatalf("frontend got %+v", re)
	}
}

func TestEmitUsesDefaultChannel(t *testing.T) {
	tm := New()
	got := make(chan tui.RoutedEvent, 1)
	go func() { got <- <-tm.Frontend() }()
	tm.Emit(contracts.Event{T: "reply", Text: "b", Done: true})
	re := <-got
	if re.Conv.ID != ChannelID || re.Event.Text != "b" {
		t.Fatalf("Emit default-channel routing wrong: %+v", re)
	}
}

func TestTerminalImplementsRoutedEventSink(t *testing.T) {
	var _ contracts.RoutedEventSink = New()
}

func TestPostEmitsReplyEvent(t *testing.T) {
	tm := New()
	got := make(chan tui.RoutedEvent, 1)
	go func() { got <- <-tm.Frontend() }()
	if _, err := tm.Post(context.Background(), contracts.Conversation{Gateway: "terminal", ID: "terminal"}, "hi"); err != nil {
		t.Fatalf("Post: %v", err)
	}
	re := <-got
	if re.Event.T != "reply" {
		t.Fatalf("Post emitted %+v, want reply", re)
	}
	if re.Event.Text != "hi" {
		t.Fatalf("Post reply text = %q, want %q", re.Event.Text, "hi")
	}
}

func TestTerminalImplementsChannelAdmin(t *testing.T) {
	var _ contracts.ChannelAdmin = New()
}

func TestCreateUnderMintsUniqueChannels(t *testing.T) {
	tm := New()
	a, err := tm.CreateUnder(context.Background(), "home", "Alpha")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := tm.CreateUnder(context.Background(), "home", "Alpha")
	if a == b {
		t.Fatalf("CreateUnder must mint unique ids, got %q twice", a)
	}
	if !strings.HasPrefix(a, "terminal/") {
		t.Fatalf("channel id %q must be terminal-namespaced", a)
	}
}

func TestArchiveEmitsCloseToTab(t *testing.T) {
	tm := New()
	got := make(chan tui.RoutedEvent, 1)
	go func() { got <- <-tm.Frontend() }()
	_ = tm.Archive(context.Background(), "terminal/x")
	re := <-got
	if re.Conv.ID != "terminal/x" || re.Event.T != "closed" {
		t.Fatalf("Archive must emit a 'closed' event to the tab: %+v", re)
	}
}

func TestGatewaySetExposesAdmin(t *testing.T) {
	set, _ := newGatewaySet(context.Background(), contracts.PluginConfig{})
	if set.Admin == nil {
		t.Fatal("terminal GatewaySet must expose ChannelAdmin")
	}
}

// fakeSessionControl is a minimal contracts.SessionControl for Dispatch and
// ensureDefaultSession tests.
type closeCall struct {
	name  string
	force bool
}

type fakeSessionControl struct {
	lastArgs    []string
	sessions    []contracts.SessionInfo
	created     []contracts.CreateSession
	closed      []closeCall
	scrollback  []contracts.ScrollbackLine
	resumed     []string
	interrupted []string
	// commandsJSON is what a `commands --json` dispatch answers, so a test can
	// stand in for the daemon registry the palette is now derived from.
	commandsJSON string
}

func (f *fakeSessionControl) Dispatch(_ context.Context, args []string) (string, error) {
	f.lastArgs = args
	if len(args) > 0 && args[0] == "commands" {
		return f.commandsJSON, nil
	}
	return "ok", nil
}

func (f *fakeSessionControl) Create(_ context.Context, spec contracts.CreateSession) (string, error) {
	f.created = append(f.created, spec)
	return "ok", nil
}

func (f *fakeSessionControl) Close(_ context.Context, name string, force bool) (string, error) {
	f.closed = append(f.closed, closeCall{name: name, force: force})
	return "ok", nil
}

func (f *fakeSessionControl) Sessions() []contracts.SessionInfo { return f.sessions }

func (f *fakeSessionControl) Scrollback(name string) []contracts.ScrollbackLine {
	return f.scrollback
}

func (f *fakeSessionControl) Resume(name string) error {
	f.resumed = append(f.resumed, name)
	return nil
}

func (f *fakeSessionControl) Interrupt(name string) bool {
	f.interrupted = append(f.interrupted, name)
	return true
}

// The terminal drives sessions by dispatch, not by pushing inbound messages, so
// the push half of the seam is inert here — present only to satisfy the port.
func (f *fakeSessionControl) Submit(string, contracts.Inbound) bool { return true }
func (f *fakeSessionControl) Pick(string, string) bool              { return true }
func (f *fakeSessionControl) Repos(context.Context) ([]contracts.RepoRef, error) {
	return nil, nil
}

func TestTerminalForwardsScrollbackAndResume(t *testing.T) {
	tm := New()
	fake := &fakeSessionControl{scrollback: []contracts.ScrollbackLine{{Role: "user", Text: "hi"}}}
	tm.BindSessionControl(fake)

	if lines := tm.Scrollback("s"); len(lines) != 1 || lines[0].Text != "hi" {
		t.Fatalf("scrollback not forwarded: %+v", lines)
	}
	if _, err := tm.Resume("s"); err != nil {
		t.Fatal(err)
	}
	if len(fake.resumed) != 1 || fake.resumed[0] != "s" {
		t.Fatalf("resume not forwarded: %+v", fake.resumed)
	}

	// unbound terminal: Scrollback nil, Resume errors (no panic).
	if lines := New().Scrollback("s"); lines != nil {
		t.Fatalf("unbound scrollback should be nil, got %+v", lines)
	}
	if _, err := New().Resume("s"); err == nil {
		t.Fatalf("unbound resume should error")
	}
}

func TestDispatchDefaultsSessionCreateToTerminal(t *testing.T) {
	tm := New()
	fake := &fakeSessionControl{}
	tm.BindSessionControl(fake)
	if _, err := tm.Dispatch([]string{"session", "create", "--name", "x"}); err != nil {
		t.Fatal(err)
	}
	for _, a := range fake.lastArgs {
		if a == "--terminal_only" {
			return
		}
	}
	t.Fatalf("--terminal_only not appended: %v", fake.lastArgs)
}

func TestDispatchRespectsExplicitGateways(t *testing.T) {
	tm := New()
	fake := &fakeSessionControl{}
	tm.BindSessionControl(fake)
	if _, err := tm.Dispatch([]string{"session", "create", "--name", "x", "--gateways", "discord"}); err != nil {
		t.Fatal(err)
	}
	for _, a := range fake.lastArgs {
		if a == "--terminal_only" {
			t.Fatalf("--terminal_only must NOT be appended when --gateways given: %v", fake.lastArgs)
		}
	}
}

func TestDispatchPassesThroughNonCreate(t *testing.T) {
	tm := New()
	fake := &fakeSessionControl{}
	tm.BindSessionControl(fake)
	if _, err := tm.Dispatch([]string{"session", "list"}); err != nil {
		t.Fatal(err)
	}
	if len(fake.lastArgs) != 2 || fake.lastArgs[0] != "session" || fake.lastArgs[1] != "list" {
		t.Fatalf("args changed for non-create: %v", fake.lastArgs)
	}
}

func TestDispatchRejectsNonSessionVerbs(t *testing.T) {
	// The TUI forwards any /-verb to Dispatch; gate the seam so daemon-management
	// verbs (which could restart the host the TUI runs in, or rewrite its config)
	// are never reachable from the terminal. Only session-scoped verbs pass.
	for _, argv := range [][]string{
		{"service", "restart"},
		{"service", "update"},
		{"set", "home", "--id", "x"},
	} {
		tm := New()
		fake := &fakeSessionControl{}
		tm.BindSessionControl(fake)
		if _, err := tm.Dispatch(argv); err == nil {
			t.Fatalf("Dispatch(%v) should be rejected from the terminal", argv)
		}
		if fake.lastArgs != nil {
			t.Fatalf("rejected verb must not reach SessionControl; got args: %v", fake.lastArgs)
		}
	}
}

func TestDispatchAllowsSessionAndAgentVerbs(t *testing.T) {
	for _, argv := range [][]string{
		{"session", "list"},
		{"session", "close", "--name", "x"},
		{"agent", "list"},
	} {
		tm := New()
		fake := &fakeSessionControl{}
		tm.BindSessionControl(fake)
		if _, err := tm.Dispatch(argv); err != nil {
			t.Fatalf("Dispatch(%v) should be allowed: %v", argv, err)
		}
		if fake.lastArgs == nil {
			t.Fatalf("allowed verb %v must reach SessionControl", argv)
		}
	}
}

// --- openDefaultSession ---

// terminalSessionNameRe mirrors the session-name guard in
// core/internal/manager/validate.go: whatever name the TUI mints must already
// pass it, since it becomes a filesystem path and a git ref downstream.
var terminalSessionNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func TestOpenDefaultSessionCreatesWhenNone(t *testing.T) {
	fake := &fakeSessionControl{} // Sessions() returns nil/empty
	name, err := openDefaultSession(context.Background(), fake)
	if err != nil {
		t.Fatalf("openDefaultSession: %v", err)
	}
	if len(fake.created) != 1 {
		t.Fatalf("expected one typed Create, got: %+v", fake.created)
	}
	spec := fake.created[0]
	if name != spec.Name {
		t.Fatalf("opened on %q but created %q", name, spec.Name)
	}
	if !terminalSessionNameRe.MatchString(spec.Name) {
		t.Fatalf("default session name %q is not a valid session slug", spec.Name)
	}
	if !spec.TerminalOnly {
		t.Fatalf("default session must be terminal-only: %+v", spec)
	}
	if !spec.Shared {
		t.Fatalf("default session must be shared: %+v", spec)
	}
}

// A launch is the start of something: it gets its own empty session even when
// the host is full of conversations, terminal-bound ones included. Landing in
// yesterday's transcript is the surprise this avoids.
func TestOpenDefaultSessionAlwaysCreates(t *testing.T) {
	fake := &fakeSessionControl{
		sessions: []contracts.SessionInfo{
			{Name: "old", ChannelID: "ch1", Gateways: []string{"terminal"}, LastTs: "2026-01-01T00:00:00Z"},
			{Name: "shelved", ChannelID: "ch2", Gateways: []string{"terminal"}, Archived: true, Resumable: true},
			{Name: "chat", ChannelID: "ch3", Gateways: []string{"discord"}, LastTs: "2026-09-01T00:00:00Z"},
		},
	}
	name, err := openDefaultSession(context.Background(), fake)
	if err != nil {
		t.Fatalf("openDefaultSession: %v", err)
	}
	if len(fake.created) != 1 || fake.created[0].Name != name {
		t.Fatalf("a launch must mint its own session; created: %+v (opened on %q)", fake.created, name)
	}
	if fake.resumed != nil {
		t.Fatalf("an archived session is /resume's business, not a launch's: %+v", fake.resumed)
	}
}

// A first launch names its tab after the directory it was started in, so the
// operator recognises it; a second one in the same directory numbers itself,
// since `session create` refuses a name already taken.
func TestDefaultSessionNameIsTheWorkingDirectory(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := slug(filepath.Base(wd))
	if got := defaultSessionName(nil); got != dir {
		t.Fatalf("defaultSessionName = %q, want the directory %q", got, dir)
	}
	taken := []contracts.SessionInfo{{Name: dir, Gateways: []string{"discord"}}}
	if got := defaultSessionName(taken); got != dir+"-2" {
		t.Fatalf("defaultSessionName = %q, want the numbered %q", got, dir+"-2")
	}
	taken = append(taken, contracts.SessionInfo{Name: dir + "-2"})
	if got := defaultSessionName(taken); got != dir+"-3" {
		t.Fatalf("defaultSessionName = %q, want the numbered %q", got, dir+"-3")
	}
	// Every name it can mint has to survive the manager's guard, the random
	// last resort included.
	for _, name := range []string{defaultSessionName(taken), randomSessionName()} {
		if !terminalSessionNameRe.MatchString(name) {
			t.Fatalf("minted name %q is not a valid session slug", name)
		}
	}
}

// The other half of minting a session per launch: one nobody spoke in is put
// away on the way out, so opening a window and closing it leaves nothing behind.
func TestArchiveIfUntouchedClosesTheMintedSession(t *testing.T) {
	tm := New()
	fake := &fakeSessionControl{
		sessions: []contracts.SessionInfo{
			{Name: "mine", ChannelID: "ch1", Gateways: []string{"terminal"}},
			{Name: "other", ChannelID: "ch2", Gateways: []string{"terminal"}},
		},
	}
	tm.BindSessionControl(fake)
	tm.baseCtx = context.Background()
	tm.mintedSession = "mine"

	tm.archiveIfUntouched()
	if len(fake.closed) != 1 || fake.closed[0].name != "mine" || fake.closed[0].force {
		t.Fatalf("untouched session not archived gently: %+v", fake.closed)
	}
	// A second pass (quit twice, or a cancel already run) must not close again.
	tm.archiveIfUntouched()
	if len(fake.closed) != 1 {
		t.Fatalf("archive ran twice: %+v", fake.closed)
	}
}

// A session that was spoken in is the operator's, whatever happened to the turn:
// LastTs is the daemon's own reading of "something is in there".
func TestArchiveIfUntouchedLeavesSpokenAndUnownedSessions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		minted  string
		session contracts.SessionInfo
	}{
		{"spoken in", "mine", contracts.SessionInfo{Name: "mine", LastTs: "2026-06-01T00:00:00Z"}},
		{"already archived", "mine", contracts.SessionInfo{Name: "mine", Archived: true}},
		{"not this window's", "", contracts.SessionInfo{Name: "mine"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tm := New()
			fake := &fakeSessionControl{sessions: []contracts.SessionInfo{tc.session}}
			tm.BindSessionControl(fake)
			tm.baseCtx = context.Background()
			tm.mintedSession = tc.minted

			tm.archiveIfUntouched()
			if fake.closed != nil {
				t.Fatalf("closed a session it does not own: %+v", fake.closed)
			}
		})
	}
}

// A hub that is already going down cannot archive anything, and a session left
// live is one the operator can still see: never a Close on a dead context.
func TestArchiveIfUntouchedSkipsWhenTheDaemonIsGone(t *testing.T) {
	tm := New()
	fake := &fakeSessionControl{sessions: []contracts.SessionInfo{{Name: "mine"}}}
	tm.BindSessionControl(fake)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tm.baseCtx = ctx
	tm.mintedSession = "mine"

	tm.archiveIfUntouched()
	if fake.closed != nil {
		t.Fatalf("Close attempted on a cancelled lifetime: %+v", fake.closed)
	}
}

// TestCommandsAdvertiseOnlyAllowedVerbs guards the palette↔Dispatch contract:
// whatever the daemon answers, the palette may only carry verbs Dispatch would
// accept — otherwise the menu offers rows that error the moment they are picked.
// The registry is faked here because the point is the filtering, not the list.
func TestCommandsAdvertiseOnlyAllowedVerbs(t *testing.T) {
	tm := New()
	fake := &fakeSessionControl{commandsJSON: `[
		{"path":["session","create"],"help":"start a session","params":[{"Name":"name","Required":true}]},
		{"path":["memory","list"],"help":"list memories"},
		{"path":["service","restart"],"help":"restart the daemon"},
		{"path":["set","home"],"help":"rewrite routing"}
	]`}
	tm.BindSessionControl(fake)
	cmds := tm.Commands()
	if len(cmds) == 0 {
		t.Fatal("Commands must advertise at least one command")
	}
	for _, c := range cmds {
		parts := strings.Fields(c.Name)
		if !terminalVerbs[parts[0]] {
			t.Fatalf("command %q leads with a verb outside terminalVerbs", c.Name)
		}
		// The CLI parser only accepts "--flag value" pairs and silently drops
		// positional tokens, so an advertised arg that is not a flag is a trap.
		if c.Args != "" && !strings.Contains(c.Args, "--") {
			t.Fatalf("command %q advertises positional args %q; the parser only accepts --flag form", c.Name, c.Args)
		}
	}
	for _, gone := range []string{"service restart", "set home"} {
		for _, c := range cmds {
			if c.Name == gone {
				t.Fatalf("%q must never reach the palette", gone)
			}
		}
	}
}

// TestCommandsWithoutABoundControlAreEmpty: a frontend that cannot reach the
// registry must show no daemon rows rather than a list typed here, which would
// be the drift this indirection exists to remove.
func TestCommandsWithoutABoundControlAreEmpty(t *testing.T) {
	if cmds := New().Commands(); len(cmds) != 0 {
		t.Fatalf("unbound Commands = %+v, want none", cmds)
	}
}
