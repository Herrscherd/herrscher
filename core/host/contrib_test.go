package host

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
	contracts "github.com/Herrscherd/herrscher-contracts"
)

// fakeContributor is a gateway that contributes commands, named by its Kind.
type fakeContributor struct {
	kind  string
	paths [][]string
}

func (f fakeContributor) Manifest() contracts.Manifest {
	return contracts.Manifest{Kind: f.kind, Category: contracts.CategoryGateway}
}

func (f fakeContributor) Commands() []contracts.Cmd {
	out := make([]contracts.Cmd, 0, len(f.paths))
	for _, p := range f.paths {
		out = append(out, contracts.New(p...).Help("fake").Do(
			func(context.Context, contracts.Input) (string, error) { return f.kind, nil },
		))
	}
	return out
}

func (f fakeContributor) Post(context.Context, contracts.Conversation, string) (contracts.MessageID, error) {
	return "", nil
}
func (f fakeContributor) Reply(context.Context, contracts.Conversation, contracts.MessageID, string) (contracts.MessageID, error) {
	return "", nil
}
func (f fakeContributor) React(context.Context, contracts.Conversation, contracts.MessageID, string) error {
	return nil
}
func (f fakeContributor) Menu(context.Context, contracts.Conversation, contracts.MessageID, string, []contracts.Choice) error {
	return nil
}

// mute is a gateway contributing nothing: the common case, which must stay free.
// It deliberately has no Commands method — embedding fakeContributor would give
// it one, and the test would then prove the assertion happens rather than that
// a non-contributor is skipped.
type mute struct{}

func (mute) Manifest() contracts.Manifest {
	return contracts.Manifest{Kind: "mute", Category: contracts.CategoryGateway}
}
func (mute) Post(context.Context, contracts.Conversation, string) (contracts.MessageID, error) {
	return "", nil
}
func (mute) Reply(context.Context, contracts.Conversation, contracts.MessageID, string) (contracts.MessageID, error) {
	return "", nil
}
func (mute) React(context.Context, contracts.Conversation, contracts.MessageID, string) error {
	return nil
}
func (mute) Menu(context.Context, contracts.Conversation, contracts.MessageID, string, []contracts.Choice) error {
	return nil
}

// This is the property the whole namespacing decision exists for: two plugins
// may declare the identical path, and both must land, told apart by their Kind.
// Without the prefix the second one would be rejected as a duplicate — the bug
// this design was written to prevent.
func TestSamePathFromTwoPluginsBothLand(t *testing.T) {
	cmds, err := CollectCommands([]contracts.GatewaySet{
		{Gateway: fakeContributor{kind: "alpha", paths: [][]string{{"channel", "read"}}}},
		{Gateway: fakeContributor{kind: "beta", paths: [][]string{{"channel", "read"}}}},
	})
	if err != nil {
		t.Fatalf("two distinct plugins must not collide: %v", err)
	}
	got := map[string]bool{}
	for _, c := range cmds {
		got[strings.Join(c.Path, " ")] = true
	}
	if !got["alpha channel read"] || !got["beta channel read"] {
		t.Fatalf("both must land under their own kind, got %v", got)
	}
}

// A plugin colliding with itself is a build mistake, and it must fail loudly
// and by name — a silently missing verb sends the operator debugging the wrong
// thing.
func TestSelfCollisionFailsByName(t *testing.T) {
	_, err := CollectCommands([]contracts.GatewaySet{
		{Gateway: fakeContributor{kind: "alpha", paths: [][]string{{"channel", "read"}, {"channel", "read"}}}},
	})
	if err == nil {
		t.Fatal("a duplicate path within one plugin must be refused")
	}
	if !strings.Contains(err.Error(), "alpha") || !strings.Contains(err.Error(), "channel read") {
		t.Fatalf("the error must name the plugin and the path: %v", err)
	}
}

// A gateway that contributes nothing is left entirely alone: it does not
// satisfy CommandSource, so it is never even asked. Most gateways are this one.
func TestNonContributorIsLeftAlone(t *testing.T) {
	cmds, err := CollectCommands([]contracts.GatewaySet{{Gateway: mute{}}})
	if err != nil {
		t.Fatalf("a plain gateway must be no trouble: %v", err)
	}
	if len(cmds) != 0 {
		t.Fatalf("a plain gateway contributes nothing, got %d", len(cmds))
	}
}

// A nil Gateway in a set must not panic: GatewaySet fields are documented as
// optional and the host degrades.
func TestNilGatewayIsSkipped(t *testing.T) {
	if _, err := CollectCommands([]contracts.GatewaySet{{}}); err != nil {
		t.Fatalf("an empty set must be skipped, not fail: %v", err)
	}
}

// A contributed command must keep working after being renamed: the prefix is
// added, nothing else is touched.
func TestPrefixingPreservesTheHandler(t *testing.T) {
	cmds, err := CollectCommands([]contracts.GatewaySet{
		{Gateway: fakeContributor{kind: "alpha", paths: [][]string{{"channel", "read"}}}},
	})
	if err != nil || len(cmds) != 1 {
		t.Fatalf("expected one command, got %d (%v)", len(cmds), err)
	}
	out, err := cmds[0].Run(context.Background(), contracts.Input{})
	if err != nil || out != "alpha" {
		t.Fatalf("the plugin's own handler must survive prefixing: %q %v", out, err)
	}
}

// A contributed command must be dispatchable under its namespaced path, which
// is the whole point: the registry is what the TUI palette and any chat
// surface both read.
func TestContributedCommandDispatches(t *testing.T) {
	reg := &cli.Registry{}
	cmds, err := CollectCommands([]contracts.GatewaySet{
		{Gateway: fakeContributor{kind: "alpha", paths: [][]string{{"channel", "read"}}}},
	})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, c := range cmds {
		if err := reg.Add(c); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	out, err := reg.Dispatch(context.Background(), []string{"alpha", "channel", "read"})
	if err != nil {
		t.Fatalf("a contributed command must dispatch: %v", err)
	}
	if out != "alpha" {
		t.Fatalf("the plugin's handler must run, got %q", out)
	}
}
