package cli_test

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
)

func leaf(path ...string) contracts.Cmd {
	return contracts.New(path...).Do(func(context.Context, contracts.Input) (string, error) {
		return "ran " + path[len(path)-1], nil
	})
}

func TestAddRejectsDuplicatePath(t *testing.T) {
	var r cli.Registry
	if err := r.Add(leaf("set", "home")); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(leaf("set", "home")); err == nil {
		t.Fatal("duplicate path must be rejected")
	}
}

func TestAddRejectsEmptyPath(t *testing.T) {
	var r cli.Registry
	if err := r.Add(contracts.New().Do(nil)); err == nil {
		t.Fatal("empty path must be rejected")
	}
}

func build(t *testing.T, cmds ...contracts.Cmd) *cli.Registry {
	t.Helper()
	var r cli.Registry
	for _, c := range cmds {
		if err := r.Add(c); err != nil {
			t.Fatal(err)
		}
	}
	return &r
}

func TestDispatchResolvesLongestPath(t *testing.T) {
	got := ""
	r := build(t,
		contracts.New("session", "list").Do(func(_ context.Context, _ contracts.Input) (string, error) {
			got = "list"
			return "", nil
		}),
		contracts.New("session", "create").Param("name", "", true).
			Do(func(_ context.Context, in contracts.Input) (string, error) {
				got = "create:" + in.Get("name")
				return "", nil
			}),
	)
	if _, err := r.Dispatch(context.Background(), []string{"session", "create", "--name", "x"}); err != nil {
		t.Fatal(err)
	}
	if got != "create:x" {
		t.Fatalf("got %q", got)
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	r := build(t, leaf("session", "list"))
	if _, err := r.Dispatch(context.Background(), []string{"nope"}); err == nil {
		t.Fatal("unknown command must error")
	}
}

func TestDispatchMissingRequiredParam(t *testing.T) {
	r := build(t, contracts.New("set", "home").Param("channel", "", true).
		Do(func(context.Context, contracts.Input) (string, error) { return "", nil }))
	if _, err := r.Dispatch(context.Background(), []string{"set", "home"}); err == nil {
		t.Fatal("missing required param must error")
	}
}

func TestDispatchBoolFlagAndRest(t *testing.T) {
	var in contracts.Input
	r := build(t, contracts.New("session", "create").
		Param("name", "", true).Param("shared", "", false).
		Do(func(_ context.Context, got contracts.Input) (string, error) { in = got; return "", nil }))
	_, err := r.Dispatch(context.Background(), []string{"session", "create", "extra", "--name", "x", "--shared"})
	if err != nil {
		t.Fatal(err)
	}
	if in.Get("name") != "x" || !in.Bool("shared") {
		t.Fatalf("args = %+v", in.Args)
	}
	if len(in.Rest) != 1 || in.Rest[0] != "extra" {
		t.Fatalf("rest = %v", in.Rest)
	}
}

func TestDispatchOptionalValueParamRequiresValueWhenPresent(t *testing.T) {
	r := build(t, contracts.New("session", "seed").
		ValueParam("turn_id", "", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			return in.Get("turn_id"), nil
		}))

	if _, err := r.Dispatch(context.Background(), []string{"session", "seed", "--turn_id"}); err == nil ||
		!strings.Contains(err.Error(), "flag --turn_id needs a value") {
		t.Fatalf("valueless optional parameter error = %v", err)
	}
	got, err := r.Dispatch(context.Background(), []string{"session", "seed", "--turn_id", "true"})
	if err != nil || got != "true" {
		t.Fatalf("explicit value true = %q, %v; want preserved", got, err)
	}
}

// The two-token form cannot carry a value that opens on "--", and must not: it
// is the missing-value mistake far more often than it is the value. --name=value
// is where a caller says it really meant one.
func TestDispatchInlineValueCarriesAFlagLookingValue(t *testing.T) {
	r := build(t, contracts.New("session", "seed").
		Param("name", "", true).Param("task", "", true).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			return in.Get("name") + "|" + in.Get("task"), nil
		}))

	if _, err := r.Dispatch(context.Background(), []string{
		"session", "seed", "--name", "s", "--task", "--no-tests please",
	}); err == nil || !strings.Contains(err.Error(), "flag --task needs a value") {
		t.Fatalf("separate-token form error = %v; want the mistake still named", err)
	}

	got, err := r.Dispatch(context.Background(), []string{
		"session", "seed", "--name", "s", "--task=--no-tests please",
	})
	if err != nil || got != "s|--no-tests please" {
		t.Fatalf("inline form = %q, %v; want the value intact", got, err)
	}
}

func TestDispatchInlineValueEdgeCases(t *testing.T) {
	r := build(t, contracts.New("memory", "record").
		Param("key", "", true).Param("body", "", false).
		Do(func(_ context.Context, in contracts.Input) (string, error) {
			return in.Get("key") + "|" + in.Get("body"), nil
		}))

	// A value carrying its own '=' splits on the first one only.
	got, err := r.Dispatch(context.Background(), []string{"memory", "record", "--key=a/b", "--body=x=1"})
	if err != nil || got != "a/b|x=1" {
		t.Fatalf("got %q, %v; want the split to stop at the first =", got, err)
	}

	// An empty inline value is a value, not a missing one.
	got, err = r.Dispatch(context.Background(), []string{"memory", "record", "--key=a/b", "--body="})
	if err != nil || got != "a/b|" {
		t.Fatalf("got %q, %v; want an empty body to count as given", got, err)
	}

	// The flag is still checked against the declared params.
	if _, err := r.Dispatch(context.Background(), []string{"memory", "record", "--nope=1"}); err == nil ||
		!strings.Contains(err.Error(), "unknown flag --nope") {
		t.Fatalf("unknown inline flag error = %v", err)
	}
}

// FlagArg reaches for the inline form only when the value needs it. Every
// released daemon parses the two-token form, and an attached CLI dials one it
// may predate — so the shape on the wire must not change for ordinary text.
func TestFlagArgUsesTheInlineFormOnlyWhenNeeded(t *testing.T) {
	if got := cli.FlagArg("text", "hello"); len(got) != 2 || got[0] != "--text" || got[1] != "hello" {
		t.Fatalf("ordinary value = %q, want the two-token form", got)
	}
	if got := cli.FlagArg("text", ""); len(got) != 2 || got[1] != "" {
		t.Fatalf("empty value = %q, want the two-token form", got)
	}
	if got := cli.FlagArg("text", "-p not a flag"); len(got) != 2 || got[1] != "-p not a flag" {
		t.Fatalf("single-dash value = %q; only -- confuses the parser", got)
	}
	if got := cli.FlagArg("task", "--no-tests"); len(got) != 1 || got[0] != "--task=--no-tests" {
		t.Fatalf("flag-looking value = %q, want the inline form", got)
	}
}

// The round trip is the actual guarantee: what FlagArg writes, parse reads back.
func TestFlagArgRoundTripsThroughDispatch(t *testing.T) {
	r := build(t, contracts.New("session", "send").
		Param("text", "", true).
		Do(func(_ context.Context, in contracts.Input) (string, error) { return in.Get("text"), nil }))

	for _, text := range []string{"hello", "--no-tests", "--", "x=1", "-p", "  spaced  ", "--a=--b"} {
		argv := append([]string{"session", "send"}, cli.FlagArg("text", text)...)
		got, err := r.Dispatch(context.Background(), argv)
		if err != nil || got != text {
			t.Fatalf("text %q came back %q, %v", text, got, err)
		}
	}
}

func TestDispatchReturnsHandlerOutput(t *testing.T) {
	r := build(t, leaf("session", "list"))
	out, err := r.Dispatch(context.Background(), []string{"session", "list"})
	if err != nil || out != "ran list" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestDispatchGlobalJSONFlag(t *testing.T) {
	r := &cli.Registry{}
	var gotJSON bool
	_ = r.Add(contracts.Cmd{
		Path:   []string{"session", "list"},
		Params: nil,
		Run: func(ctx context.Context, in contracts.Input) (string, error) {
			gotJSON = in.JSON
			return "ok", nil
		},
	})
	if _, err := r.Dispatch(context.Background(), []string{"session", "list", "--json"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !gotJSON {
		t.Fatal("expected in.JSON == true when --json passed")
	}
}

func TestHelpRendersSortedUsage(t *testing.T) {
	r := build(t,
		contracts.New("set", "home").Help("set home").Param("channel", "", true).
			Do(func(context.Context, contracts.Input) (string, error) { return "", nil }),
		contracts.New("session", "list").Help("list sessions").
			Do(func(context.Context, contracts.Input) (string, error) { return "", nil }),
	)
	h := r.Help()
	// sorted: "session list" sorts before "set home"
	if want := "  session list"; !strings.Contains(h, want) {
		t.Fatalf("help missing %q:\n%s", want, h)
	}
	if !strings.Contains(h, "--channel <channel>") {
		t.Fatalf("help missing required-param rendering:\n%s", h)
	}
}

func TestRunInvokesCommandByExactPathWithTypedInput(t *testing.T) {
	var gotName string
	r := build(t,
		contracts.New("session", "create").Param("name", "", true).
			Do(func(_ context.Context, in contracts.Input) (string, error) {
				gotName = in.Get("name")
				return "created", nil
			}),
	)
	out, err := r.Run(context.Background(), []string{"session", "create"},
		contracts.Input{Args: map[string]string{"name": "main"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "created" || gotName != "main" {
		t.Fatalf("Run did not pass typed input: out=%q name=%q", out, gotName)
	}
}

func TestRunRejectsUnknownPath(t *testing.T) {
	r := build(t, leaf("session", "list"))
	if _, err := r.Run(context.Background(), []string{"session", "nope"}, contracts.Input{}); err == nil {
		t.Fatal("Run must reject an unknown path")
	}
}

func TestRunChecksRequiredParams(t *testing.T) {
	r := build(t,
		contracts.New("session", "create").Param("name", "", true).
			Do(func(context.Context, contracts.Input) (string, error) { return "", nil }),
	)
	if _, err := r.Run(context.Background(), []string{"session", "create"}, contracts.Input{Args: map[string]string{}}); err == nil {
		t.Fatal("Run must reject missing required param")
	}
}

// TestSpecsDescribeTheWholeRegistry: the palette is derived from this, so a
// command missing here is a command an operator cannot discover.
func TestSpecsDescribeTheWholeRegistry(t *testing.T) {
	var r cli.Registry
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	okRun := func(context.Context, contracts.Input) (string, error) { return "", nil }
	must(r.Add(contracts.New("zulu").Help("last").Do(okRun)))
	must(r.Add(contracts.New("alpha", "one").
		Help("first").
		Param("name", "who", true).
		Do(okRun)))

	specs := r.Specs()
	if len(specs) != 2 {
		t.Fatalf("Specs = %+v, want both commands", specs)
	}
	// Sorted by path, so a menu built from this is stable between boots.
	if got := strings.Join(specs[0].Path, " "); got != "alpha one" {
		t.Fatalf("first spec = %q, want the alphabetical one", got)
	}
	if specs[0].Help != "first" || len(specs[0].Params) != 1 || specs[0].Params[0].Name != "name" {
		t.Fatalf("spec dropped declared shape: %+v", specs[0])
	}
}

func TestResolveReadsThePathAndTheFlagsWithoutRunning(t *testing.T) {
	ran := false
	var reg cli.Registry
	if err := reg.Add(contracts.New("session", "send").
		Param("name", "session name", true).
		Do(func(context.Context, contracts.Input) (string, error) {
			ran = true
			return "", nil
		})); err != nil {
		t.Fatal(err)
	}
	path, in, ok := reg.Resolve([]string{"session", "send", "--name", "revue", "hello"})
	if !ok {
		t.Fatal("Resolve must find a registered command")
	}
	if strings.Join(path, " ") != "session send" {
		t.Fatalf("path = %v, want [session send]", path)
	}
	if in.Get("name") != "revue" {
		t.Fatalf("name = %q, want revue", in.Get("name"))
	}
	if len(in.Rest) != 1 || in.Rest[0] != "hello" {
		t.Fatalf("Rest = %v, want [hello]", in.Rest)
	}
	if ran {
		t.Fatal("Resolve must not run the command")
	}
}

func TestResolveReportsAnUnknownCommand(t *testing.T) {
	var reg cli.Registry
	if _, _, ok := reg.Resolve([]string{"quantum", "entangle"}); ok {
		t.Fatal("Resolve must not claim to have found an unregistered command")
	}
}

// A missing required flag is Dispatch's error to report. Resolve still names
// the path, because the caller asked for that verb whatever it forgot to pass,
// and a caller deciding whether the verb is allowed must not be told nothing.
func TestResolveNamesThePathEvenWhenTheFlagsDoNotParse(t *testing.T) {
	var reg cli.Registry
	if err := reg.Add(contracts.New("host", "add").
		Param("ssh", "ssh target", true).
		Do(func(context.Context, contracts.Input) (string, error) { return "", nil })); err != nil {
		t.Fatal(err)
	}
	path, _, ok := reg.Resolve([]string{"host", "add"})
	if !ok {
		t.Fatal("Resolve must find the command")
	}
	if strings.Join(path, " ") != "host add" {
		t.Fatalf("path = %v, want [host add]", path)
	}
}
